package log

import (
	"fmt"
	"github.com/sequix/grok_exporter/config/v2"
	"github.com/sirupsen/logrus"
	"io"
	"os"
)

var (
	globalLogger logrus.FieldLogger
)

func init() {
	jsonFmter := &logrus.JSONFormatter{}

	logrus.SetOutput(os.Stdout)
	logrus.SetFormatter(jsonFmter)
	logrus.SetLevel(logrus.InfoLevel)

	lg := logrus.New()
	lg.SetOutput(os.Stdout)
	lg.SetFormatter(jsonFmter)
	lg.SetLevel(logrus.InfoLevel)
	globalLogger = lg
}

func New() logrus.FieldLogger {
	return globalLogger
}

func Init(cfg *v2.Config) (logrus.FieldLogger, error) {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})

	switch cfg.Global.LogTo {
	case "file":
		logger.SetOutput(&cfg.LogRotate)
	case "stdout":
		logger.SetOutput(os.Stdout)
	case "mixed":
		w := io.MultiWriter(os.Stdout, &cfg.LogRotate)
		logger.SetOutput(w)
	default:
		return nil, fmt.Errorf("unknown log_to type: %q", cfg.Global.LogTo)
	}

	logLevel, err := logrus.ParseLevel(cfg.Global.LogLevel)
	if err != nil {
		return nil, err
	}
	logger.SetLevel(logLevel)
	globalLogger = logger
	return logger, nil
}

func WithField(key string, value interface{}) *logrus.Entry {
	return globalLogger.WithField(key, value)
}

func WithFields(fields logrus.Fields) *logrus.Entry {
	return globalLogger.WithFields(fields)
}

func WithError(err error) *logrus.Entry {
	return globalLogger.WithError(err)
}

func Debugf(format string, args ...interface{}) {
	globalLogger.Debugf(format, args...)
}

func Infof(format string, args ...interface{}) {
	globalLogger.Infof(format, args...)
}

func Printf(format string, args ...interface{}) {
	globalLogger.Printf(format, args...)
}

func Warnf(format string, args ...interface{}) {
	globalLogger.Warnf(format, args...)
}

func Warningf(format string, args ...interface{}) {
	globalLogger.Warningf(format, args...)
}

func Errorf(format string, args ...interface{}) {
	globalLogger.Errorf(format, args...)
}

func Fatalf(format string, args ...interface{}) {
	globalLogger.Fatalf(format, args...)
}

func Panicf(format string, args ...interface{}) {
	globalLogger.Panicf(format, args...)
}

func Debug(args ...interface{}) {
	globalLogger.Debug(args...)
}

func Info(args ...interface{}) {
	globalLogger.Info(args...)
}

func Print(args ...interface{}) {
	globalLogger.Print(args...)
}

func Warn(args ...interface{}) {
	globalLogger.Warn(args...)
}

func Warning(args ...interface{}) {
	globalLogger.Warning(args...)
}

func Error(args ...interface{}) {
	globalLogger.Error(args...)
}

func Fatal(args ...interface{}) {
	globalLogger.Fatal(args...)
}

func Panic(args ...interface{}) {
	globalLogger.Panic(args...)
}

func Debugln(args ...interface{}) {
	globalLogger.Debugln(args...)
}

func Infoln(args ...interface{}) {
	globalLogger.Infoln(args...)
}

func Println(args ...interface{}) {
	globalLogger.Println(args...)
}

func Warnln(args ...interface{}) {
	globalLogger.Warnln(args...)
}

func Warningln(args ...interface{}) {
	globalLogger.Warningln(args...)
}

func Errorln(args ...interface{}) {
	globalLogger.Errorln(args...)
}

func Fatalln(args ...interface{}) {
	globalLogger.Fatalln(args...)
}

func Panicln(args ...interface{}) {
	globalLogger.Panic(args...)
}
