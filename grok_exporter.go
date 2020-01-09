// Copyright 2016-2018 The grok_exporter Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"github.com/sequix/grok_exporter/config"
	"github.com/sequix/grok_exporter/config/v2"
	"github.com/sequix/grok_exporter/exporter"
	"github.com/sequix/grok_exporter/oniguruma"
	"github.com/sequix/grok_exporter/tailer"
	"github.com/sequix/grok_exporter/tailer/fswatcher"
	"github.com/sequix/grok_exporter/tailer/glob"
	"github.com/sequix/grok_exporter/tailer/position"
)

var (
	printVersion = flag.Bool("version", false, "Print the grok_exporter version.")
	configPath   = flag.String("config", "", "Path to the config file. Try '-config ./example/config.yml' to get started.")
	showConfig   = flag.Bool("showconfig", false, "Print the current configuration to the console. Example: 'grok_exporter -showconfig -config ./example/config.yml'")
	logLevel     = flag.String("loglevel", "", "log level: panic, fatal, error, warn, info, debug, trace")
)

const (
	number_of_lines_matched_label = "matched"
	number_of_lines_ignored_label = "ignored"
)

func main() {
	flag.Parse()
	if *printVersion {
		fmt.Printf("%v\n", exporter.VersionString())
		return
	}
	validateCommandLineOrExit()
	cfg, warn, err := config.LoadConfigFile(*configPath)
	exitOnError(err)
	if len(warn) > 0 && !*showConfig {
		// warning is suppressed when '-showconfig' is used
		fmt.Fprintf(os.Stderr, "%v\n", warn)
	}
	cfg.LoadEnvironments()
	if len(*logLevel) > 0 {
		cfg.Global.LogLevel = *logLevel
	}
	if *showConfig {
		fmt.Printf("%v\n", cfg)
		return
	}
	patterns, err := initPatterns(cfg)
	exitOnError(err)
	metrics, err := createMetrics(cfg, patterns)
	exitOnError(err)
	for _, m := range metrics {
		prometheus.MustRegister(m.Collector())
	}
	nLinesTotal, nMatchesByMetric, procTimeMicrosecondsByMetric, nErrorsByMetric := initSelfMonitoring(metrics)

	logger, err := initLogger(cfg)
	exitOnError(err)

	tail, err := startTailer(cfg, logger)
	exitOnError(err)

	// gather up the handlers with which to start the webserver
	httpHandlers := []exporter.HttpServerPathHandler{}
	httpHandlers = append(httpHandlers, exporter.HttpServerPathHandler{
		Path:    cfg.Server.Path,
		Handler: prometheus.Handler()})
	if cfg.Input.Type == "webhook" {
		httpHandlers = append(httpHandlers, exporter.HttpServerPathHandler{
			Path:    cfg.Input.WebhookPath,
			Handler: tailer.WebhookHandler()})
	}

	fmt.Print(startMsg(cfg, httpHandlers))
	serverErrors := startServer(cfg.Server, httpHandlers)

	retentionTicker := time.NewTicker(cfg.Global.RetentionCheckInterval)

	for {
		select {
		case err := <-serverErrors:
			exitOnError(fmt.Errorf("server error: %v", err.Error()))
		case err := <-tail.Errors():
			if err.Type() == fswatcher.Structured {
				errS := err.(*fswatcher.StructuredError)
				logger.WithField("err", errS.Cause()).WithFields(errS.KVs).Error(errS.Error())
				continue
			}
			logger.WithField("err", err).Error(err.Error())
		case line := <-tail.Lines():
			matched := false
			for _, metric := range metrics {
				start := time.Now()
				if !metric.MatchPath(line.File) {
					continue
				}
				match, err := metric.ProcessMatch(line.Line)
				if err != nil {
					logger.WithFields(map[string]interface{}{
						"line": line.Line,
						"err":  err,
					}).Warn("process matching, skip log line")
					nErrorsByMetric.WithLabelValues(metric.Name()).Inc()
				}
				if match != nil {
					nMatchesByMetric.WithLabelValues(metric.Name()).Inc()
					procTimeMicrosecondsByMetric.WithLabelValues(metric.Name()).Add(float64(time.Since(start).Nanoseconds() / int64(1000)))
					matched = true
				}
				_, err = metric.ProcessDeleteMatch(line.Line)
				if err != nil {
					logger.WithFields(map[string]interface{}{
						"line": line.Line,
						"err":  err,
					}).Warn("process delete match, skip log line")
					nErrorsByMetric.WithLabelValues(metric.Name()).Inc()
				}
				// TODO: create metric to monitor number of matching delete_patterns
			}
			if matched {
				nLinesTotal.WithLabelValues(number_of_lines_matched_label).Inc()
			} else {
				nLinesTotal.WithLabelValues(number_of_lines_ignored_label).Inc()
			}
		case <-retentionTicker.C:
			for _, metric := range metrics {
				err = metric.ProcessRetention()
				if err != nil {
					fmt.Fprintf(os.Stderr, "WARNING: error while processing retention on metric %v: %v", metric.Name(), err)
					nErrorsByMetric.WithLabelValues(metric.Name()).Inc()
				}
			}
			// TODO: create metric to monitor number of metrics cleaned up via retention
		}
	}
}

func initLogger(cfg *v2.Config) (logrus.FieldLogger, error) {
	jsonFmter := &logrus.JSONFormatter{}

	logger := logrus.New()
	logrus.SetFormatter(jsonFmter)
	logger.SetFormatter(jsonFmter)

	switch cfg.Global.LogTo {
	case "file":
		logrus.SetOutput(&cfg.LogRotate)
		logger.SetOutput(&cfg.LogRotate)
	case "stdout":
		logrus.SetOutput(os.Stdout)
		logger.SetOutput(os.Stdout)
	case "mixed":
		w := io.MultiWriter(os.Stdout, &cfg.LogRotate)
		logrus.SetOutput(w)
		logger.SetOutput(w)
	default:
		return nil, fmt.Errorf("unknown log_to type: %q", cfg.Global.LogTo)
	}

	logLevel, err := logrus.ParseLevel(cfg.Global.LogLevel)
	if err != nil {
		return nil, err
	}
	logrus.SetLevel(logLevel)
	logger.SetLevel(logLevel)
	return logger, nil
}

func startMsg(cfg *v2.Config, httpHandlers []exporter.HttpServerPathHandler) string {
	host := "localhost"
	if len(cfg.Server.Host) > 0 {
		host = cfg.Server.Host
	} else {
		hostname, err := os.Hostname()
		if err == nil {
			host = hostname
		}
	}

	var sb strings.Builder
	baseUrl := fmt.Sprintf("%v://%v:%v", cfg.Server.Protocol, host, cfg.Server.Port)
	sb.WriteString("Starting server on")
	for _, httpHandler := range httpHandlers {
		sb.WriteString(fmt.Sprintf(" %v%v", baseUrl, httpHandler.Path))
	}
	sb.WriteString("\n")
	return sb.String()
}

func exitOnError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err.Error())
		os.Exit(-1)
	}
}

func validateCommandLineOrExit() {
	if len(*configPath) == 0 {
		if *showConfig {
			fmt.Fprint(os.Stderr, "Usage: grok_exporter -showconfig -config <path>\n")
		} else {
			fmt.Fprint(os.Stderr, "Usage: grok_exporter -config <path>\n")
		}
		os.Exit(-1)
	}
}

func initPatterns(cfg *v2.Config) (*exporter.Patterns, error) {
	patterns := exporter.InitPatterns()
	if len(cfg.Grok.PatternsDir) > 0 {
		err := patterns.AddDir(cfg.Grok.PatternsDir)
		if err != nil {
			return nil, err
		}
	}
	for _, pattern := range cfg.Grok.AdditionalPatterns {
		err := patterns.AddPattern(pattern)
		if err != nil {
			return nil, err
		}
	}
	return patterns, nil
}

func createMetrics(cfg *v2.Config, patterns *exporter.Patterns) ([]*exporter.PathMetric, error) {
	result := make([]*exporter.PathMetric, 0, len(cfg.Metrics))
	for _, m := range cfg.Metrics {
		var (
			regex, deleteRegex *oniguruma.Regex
			err                error
		)
		regex, err = exporter.Compile(m.Match, patterns)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize metric %v: %v", m.Name, err.Error())
		}
		if len(m.DeleteMatch) > 0 {
			deleteRegex, err = exporter.Compile(m.DeleteMatch, patterns)
			if err != nil {
				return nil, fmt.Errorf("failed to initialize metric %v: %v", m.Name, err.Error())
			}
		}
		err = exporter.VerifyFieldNames(&m, regex, deleteRegex)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize metric %v: %v", m.Name, err.Error())
		}
		path, err := globsFromPathes(m.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize metric %v: %v", m.Name, err.Error())
		}
		excludes, err := globsFromPathes(m.Excludes)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize metric %v: %v", m.Name, err.Error())
		}
		switch m.Type {
		case "counter":
			mt := exporter.NewCounterMetric(&m, regex, deleteRegex)
			result = append(result, exporter.NewPathMatchMetric(mt, path, excludes))
		case "gauge":
			mt := exporter.NewGaugeMetric(&m, regex, deleteRegex)
			result = append(result, exporter.NewPathMatchMetric(mt, path, excludes))
		case "histogram":
			mt := exporter.NewHistogramMetric(&m, regex, deleteRegex)
			result = append(result, exporter.NewPathMatchMetric(mt, path, excludes))
		case "summary":
			mt := exporter.NewSummaryMetric(&m, regex, deleteRegex)
			result = append(result, exporter.NewPathMatchMetric(mt, path, excludes))
		default:
			return nil, fmt.Errorf("Failed to initialize metrics: Metric type %v is not supported.", m.Type)
		}
	}
	return result, nil
}

func initSelfMonitoring(metrics []*exporter.PathMetric) (*prometheus.CounterVec, *prometheus.CounterVec, *prometheus.CounterVec, *prometheus.CounterVec) {
	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "grok_exporter_build_info",
		Help: "A metric with a constant '1' value labeled by version, builddate, branch, revision, goversion, and platform on which grok_exporter was built.",
	}, []string{"version", "builddate", "branch", "revision", "goversion", "platform"})
	nLinesTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "grok_exporter_lines_total",
		Help: "Total number of log lines processed by grok_exporter.",
	}, []string{"status"})
	nMatchesByMetric := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "grok_exporter_lines_matching_total",
		Help: "Number of lines matched for each metric. Note that one line can be matched by multiple metrics.",
	}, []string{"metric"})
	procTimeMicrosecondsByMetric := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "grok_exporter_lines_processing_time_microseconds_total",
		Help: "Processing time in microseconds for each metric. Divide by grok_exporter_lines_matching_total to get the averge processing time for one log line.",
	}, []string{"metric"})
	nErrorsByMetric := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "grok_exporter_line_processing_errors_total",
		Help: "Number of errors for each metric. If this is > 0 there is an error in the configuration file. Check grok_exporter's console output.",
	}, []string{"metric"})

	prometheus.MustRegister(buildInfo)
	prometheus.MustRegister(nLinesTotal)
	prometheus.MustRegister(nMatchesByMetric)
	prometheus.MustRegister(procTimeMicrosecondsByMetric)
	prometheus.MustRegister(nErrorsByMetric)

	buildInfo.WithLabelValues(exporter.Version, exporter.BuildDate, exporter.Branch, exporter.Revision, exporter.GoVersion, exporter.Platform).Set(1)
	// Initializing a value with zero makes the label appear. Otherwise the label is not shown until the first value is observed.
	nLinesTotal.WithLabelValues(number_of_lines_matched_label).Add(0)
	nLinesTotal.WithLabelValues(number_of_lines_ignored_label).Add(0)
	for _, metric := range metrics {
		nMatchesByMetric.WithLabelValues(metric.Name()).Add(0)
		procTimeMicrosecondsByMetric.WithLabelValues(metric.Name()).Add(0)
		nErrorsByMetric.WithLabelValues(metric.Name()).Add(0)
	}
	return nLinesTotal, nMatchesByMetric, procTimeMicrosecondsByMetric, nErrorsByMetric
}

func startServer(cfg v2.ServerConfig, httpHandlers []exporter.HttpServerPathHandler) chan error {
	serverErrors := make(chan error)
	go func() {
		switch {
		case cfg.Protocol == "http":
			serverErrors <- exporter.RunHttpServer(cfg.Host, cfg.Port, httpHandlers)
		case cfg.Protocol == "https":
			if cfg.Cert != "" && cfg.Key != "" {
				serverErrors <- exporter.RunHttpsServer(cfg.Host, cfg.Port, cfg.Cert, cfg.Key, httpHandlers)
			} else {
				serverErrors <- exporter.RunHttpsServerWithDefaultKeys(cfg.Host, cfg.Port, httpHandlers)
			}
		default:
			// This cannot happen, because cfg.validate() makes sure that protocol is either http or https.
			serverErrors <- fmt.Errorf("Configuration error: Invalid 'server.protocol': '%v'. Expecting 'http' or 'https'.", cfg.Protocol)
		}
	}()
	return serverErrors
}

func startTailer(cfg *v2.Config, logger logrus.FieldLogger) (fswatcher.Interface, error) {
	var tail fswatcher.Interface

	gs, err := globsFromPathes(cfg.Input.Path)
	if err != nil {
		return nil, err
	}

	excludes, err := globsFromPathes(cfg.Input.Excludes)
	if err != nil {
		return nil, err
	}

	switch {
	case cfg.Input.Type == "file":
		pos, err := position.New(logger, cfg.Input.PositionFile, cfg.Input.SyncInterval)
		if err != nil {
			return nil, err
		}
		if cfg.Input.CollectMode == "mixed" {
			logger.Infof("Start watching %v, excludes %v", cfg.Input.Path, cfg.Input.Excludes)
			tail, err = fswatcher.RunFileTailer(
				gs,
				excludes,
				pos,
				cfg.Input.MaxLineSize,
				cfg.Input.MaxLinesRatePerFile,
				cfg.Input.PollInterval,
				cfg.Input.IdleTimeout,
				logger,
			)
		} else if cfg.Input.CollectMode == "poll" {
			logger.Infof("Start polling for %q", cfg.Input.Path)
			tail, err = fswatcher.RunPollingFileTailer(
				gs,
				excludes,
				pos,
				cfg.Input.PollInterval,
				cfg.Input.IdleTimeout,
				logger,
			)
		} else {
			return nil, fmt.Errorf("unknown collect mode %q", cfg.Input.CollectMode)
		}
		if err != nil {
			return nil, err
		}
	case cfg.Input.Type == "stdin":
		tail = tailer.RunStdinTailer()
	case cfg.Input.Type == "webhook":
		tail = tailer.InitWebhookTailer(&cfg.Input)
	default:
		return nil, fmt.Errorf("Config error: Input type '%v' unknown.", cfg.Input.Type)
	}
	bufferLoadMetric := exporter.NewBufferLoadMetric(logger, cfg.Input.MaxLinesInBuffer > 0)
	return tailer.BufferedTailerWithMetrics(tail, bufferLoadMetric, logger, cfg.Input.MaxLinesInBuffer), nil
}

func globsFromPathes(pathes []string) ([]glob.Glob, error) {
	gs := make([]glob.Glob, 0, len(pathes))
	for _, path := range pathes {
		g, err := glob.FromPath(path)
		if err != nil {
			return nil, err
		}
		gs = append(gs, g)
	}
	return gs, nil
}
