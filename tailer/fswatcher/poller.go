package fswatcher

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/sequix/grok_exporter/tailer/glob"
	"github.com/sequix/grok_exporter/tailer/position"
)

// TODO file idle timeout

type poller struct {
	pos               position.Interface
	failOnMissingFile bool
	globs             []glob.Glob
	logger            logrus.FieldLogger
	pollInterval      time.Duration
	pollingDirs       map[string]struct{}
	pollingFiles      map[string]*file
	lines             chan *Line
	errors            chan Error
	done              chan struct{}
	terminated        chan struct{}
}

func RunPollingFileTailer(
	globs []glob.Glob,
	pos position.Interface,
	failOnMissingFile bool,
	pollInterval time.Duration,
	fileIdleTimeout time.Duration,
	log logrus.FieldLogger,
) (Interface, error) {
	dirs, Err := expandGlobs(globs)
	if Err != nil {
		return nil, Err
	}

	p := &poller{
		pos:               pos,
		failOnMissingFile: failOnMissingFile,
		globs:             globs,
		logger:            log.WithField("component", "poller"),
		pollInterval:      pollInterval,
		pollingDirs:       dirs,
		pollingFiles:      make(map[string]*file),
		lines:             make(chan *Line),
		errors:            make(chan Error),
		done:              make(chan struct{}),
		terminated:        make(chan struct{}),
	}
	go p.run()
	return p, nil
}

func (p *poller) Lines() chan *Line {
	return p.lines
}

func (p *poller) Errors() chan Error {
	return p.errors
}

func (p *poller) Close() {
	close(p.done)
	<- p.terminated
}

func (p *poller) run() {
	defer func() { close(p.terminated) }()
	tick := time.NewTimer(p.pollInterval)	// 直接在for中写 time.After 会造成内存泄露
	defer tick.Stop()
	for {
		tick.Reset(p.pollInterval)
		select {
		case <-tick.C:
			p.stopFiles()
			p.relist()
			p.startFiles()
		case <-p.done:
			p.stopFiles()
			close(p.lines)
			close(p.errors)
			return
		}
	}
}

// 重新listdir，获取所有需要监听的文件
func (p *poller) relist() {
	newPollingFiles := make(map[string]*file)
	for dir := range p.pollingDirs {
		fis, err := ioutil.ReadDir(dir)
		if err != nil {
			p.errors <- NewError(NotSpecified, err, fmt.Sprintf("read dir %s", dir))
			continue
		}
		for _, fi := range fis {
			path := filepath.Join(dir, fi.Name())
			if !matchGlobs(path, p.globs) {
				continue
			}
			f, ok := p.pollingFiles[path]
			if !ok {
				f, err = p.newFile(path)
				if err != nil {
					errType := NotSpecified
					if p.failOnMissingFile && os.IsNotExist(err) {
						errType = FileNotFound
					}
					p.errors <- NewErrorf(ErrorType(errType), err, "open file %s", path)
					continue
				}
			}
			newPollingFiles[path] = f
		}
	}
	p.pollingFiles = newPollingFiles
}

func (p *poller) startFiles() {
	for _, f := range p.pollingFiles {
		go f.run()
	}
}

func (p *poller) stopFiles() {
	for _, f := range p.pollingFiles {
		f.stop()
	}
	p.pollingFiles = make(map[string]*file)
}
