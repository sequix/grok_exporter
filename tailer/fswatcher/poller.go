package fswatcher

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/fstab/grok_exporter/tailer/glob"
)

type poller struct {
	readall           bool
	failOnMissingFile bool
	globs             []glob.Glob
	logger            logrus.FieldLogger
	pollInterval      time.Duration
	watchedDirs       map[string]struct{}
	watchedFiles      map[string]*file
	lines             chan *Line
	errors            chan Error
	done              chan struct{}
}

func RunPollingFileTailer(globs []glob.Glob, readall bool, failOnMissingFile bool, pollInterval time.Duration, log logrus.FieldLogger) (Interface, error) {
	dirs, Err := expandGlobs(globs)
	if Err != nil {
		return nil, Err
	}

	p := &poller{
		readall:           readall,
		failOnMissingFile: failOnMissingFile,
		globs:             globs,
		logger:            log.WithField("component", "poller"),
		pollInterval:      pollInterval,
		watchedDirs:       dirs,
		watchedFiles:      make(map[string]*file),
		lines:             make(chan *Line),
		errors:            make(chan Error),
		done:              make(chan struct{}),
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
}

func (p *poller) run() {
	// 直接在for中写 time.After 会造成内存泄露
	poll := time.NewTimer(p.pollInterval)
	defer poll.Stop()
	for {
		poll.Reset(p.pollInterval)
		select {
		case <-poll.C:
			p.sync()
		case <-p.done:
			return
		}
	}
}

// 重新listdir，获取所有需要监听的文件
func (p *poller) sync() {
	newWatchedFiles := make(map[string]*file)
	for dir := range p.watchedDirs {
		fis, err := ioutil.ReadDir(dir)
		if err != nil {
			p.errors <- NewError(NotSpecified, err, fmt.Sprintf("read dir %s failed", dir))
			continue
		}
		for _, fi := range fis {
			path := filepath.Join(dir, fi.Name())
			if !matchGlobs(path, p.globs) {
				continue
			}
			f, ok := p.watchedFiles[path]
			if !ok {
				f, err = newFile(path, p.readall)
				if err != nil {
					errType := NotSpecified
					if p.failOnMissingFile && os.IsNotExist(err) {
						errType = FileNotFound
					}
					p.errors <- NewErrorf(ErrorType(errType), err, "open file %s failed", path)
					continue
				}
			}
			newWatchedFiles[path] = f
		}
	}
	p.watchedFiles = newWatchedFiles

	wg := &sync.WaitGroup{}
	wg.Add(len(newWatchedFiles))

	for path, f := range newWatchedFiles {
		go func() {
			p.logger.Debug(fmt.Sprintf("reading file %s", path))
			err := p.readlineUntilEOF(f)
			if err != nil {
				p.errors <- NewErrorf(NotSpecified, err, "read file %s failed", f.path)
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func (p *poller) readlineUntilEOF(f *file) error {
	for {
		line, err := f.readline()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		p.lines <- line
	}
}
