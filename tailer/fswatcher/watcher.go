// Copyright 2019 The grok_exporter Authors
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

package fswatcher

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sequix/tail"
	"github.com/sequix/tail/ratelimiter"
	"github.com/sirupsen/logrus"

	"github.com/sequix/grok_exporter/tailer/glob"
	"github.com/sequix/grok_exporter/tailer/position"
	"github.com/sequix/grok_exporter/util"
)

type watcher struct {
	pos         position.Interface
	globs       []glob.Glob
	excludes    []glob.Glob
	tailConfig  tail.Config
	idleTimeout time.Duration
	logger      logrus.FieldLogger
	watcher     *fsnotify.Watcher
	tailers     map[string]*tailer
	lines       chan *Line
	errors      chan Error
	done        chan struct{}
	terminated  chan struct{}
}

func RunFileTailer(
	globs []glob.Glob,
	excludes []glob.Glob,
	pos position.Interface,
	maxLineSize int,
	maxLinesPerSeconds uint16,
	pollInterval time.Duration,
	fileIdleTimeout time.Duration,
	log logrus.FieldLogger,
) (Interface, error) {
	dirs, Err := expandGlobs(globs)
	if Err != nil {
		return nil, Err
	}

	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	tailConfig := tail.Config{
		Location: &tail.SeekInfo{
			Offset: 0,
			Whence: io.SeekStart,
		},
		ReOpen:       true,
		Follow:       true,
		PollInterval: pollInterval,
		MaxLineSize:  maxLineSize,
		MustExist:    false,
	}

	// TODO 不掉日志的速率限制
	if maxLinesPerSeconds > 0 {
		tailConfig.RateLimiter = ratelimiter.NewLeakyBucket(maxLinesPerSeconds, time.Second)
	}

	w := &watcher{
		pos:         pos,
		globs:       globs,
		excludes:    excludes,
		tailConfig:  tailConfig,
		idleTimeout: fileIdleTimeout,
		logger:      log.WithField("component", "watcher"),
		watcher:     fw,
		tailers:     map[string]*tailer{},
		lines:       make(chan *Line),
		errors:      make(chan Error),
		done:        make(chan struct{}),
		terminated:  make(chan struct{}),
	}
	w.init(dirs)
	if w.idleTimeout == 0 {
		go w.runWithoutCleaner()
	} else {
		go w.run()
	}
	return w, nil
}

func (w *watcher) run() {
	defer close(w.terminated)

	ticker := time.NewTicker(w.idleTimeout)
	defer ticker.Stop()

	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				continue
			}
			w.logger.WithField("event", event).Debug("recv event")
			w.handle(event)
		case now := <-ticker.C:
			w.cleanIdleFiles(now)
		case <-w.done:
			for _, t := range w.tailers {
				t.stop(false)
			}
			close(w.lines)
			close(w.errors)
			return
		}
	}
}

func (w *watcher) runWithoutCleaner() {
	defer close(w.terminated)

	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				continue
			}
			w.logger.WithField("event", event).Debug("recv event")
			w.handle(event)
		case <-w.done:
			for _, t := range w.tailers {
				t.stop(false)
			}
			close(w.lines)
			close(w.errors)
			return
		}
	}
}

func (w *watcher) shouldWatch(path string) bool {
	return util.MatchGlobs(path, w.globs) && !util.MatchGlobs(path, w.excludes)
}

// list pollingDirs，获取所有需要监听的文件
func (w *watcher) init(dirs map[string]struct{}) {
	for dir := range dirs {
		fis, err := ioutil.ReadDir(dir)
		if err != nil {
			w.errors <- NewStructuredError(err, "read dir", map[string]interface{}{"path": dir})
			continue
		}
		if err := w.watcher.Add(dir); err != nil {
			w.errors <- NewStructuredError(err, "watch new dir", map[string]interface{}{"path": dir})
			continue
		}
		for _, fi := range fis {
			path := filepath.Join(dir, fi.Name())
			if w.shouldWatch(path) {
				w.watch(path)
			}
		}
	}
}

func (w *watcher) handle(event fsnotify.Event) {
	path := event.Name
	ops := strings.Split(event.Op.String(), "|")
	for _, op := range ops {
		switch op {
		case "CREATE":
			if w.shouldWatch(path) {
				w.watch(path)
			}
		case "CHMOD":
			if w.shouldWatch(path) {
				f, err := os.OpenFile(path, os.O_RDONLY, 0666)
				if err != nil {
					if os.IsPermission(err) {
						w.unwatch(path, false)
					}
					continue
				}
				f.Close()
			}
		case "RENAME":
			w.unwatch(path, false)
		case "REMOVE":
			w.unwatch(path, true)
		}
	}
}

func (w *watcher) watch(path string) {
	if _, existing := w.tailers[path]; existing {
		return
	}
	w.logger.WithField("path", path).Info("watch new file")
	t, err := w.newTailer(path)
	if err != nil {
		w.errors <- NewStructuredError(err, "watch new file", map[string]interface{}{"path": path})
		return
	}
	w.tailers[path] = t
	go t.run()
}

func (w *watcher) unwatch(path string, delPos bool) {
	t, ok := w.tailers[path]
	if !ok {
		return
	}

	w.logger.WithFields(map[string]interface{}{
		"path":   path,
		"delPos": delPos,
	}).Info("unwatch file")

	t.stop(delPos)
	delete(w.tailers, path)
}

func (w *watcher) cleanIdleFiles(now time.Time) {
	newTailers := make(map[string]*tailer)
	for k, t := range w.tailers {
		readAt := t.readAt.Load().(time.Time)
		if now.Sub(readAt) >= w.idleTimeout {
			w.logger.WithField("path", t.path).Info("file timeout")
			t.stop(false)
			continue
		}
		newTailers[k] = t
	}
	w.tailers = newTailers
}

func (w *watcher) delPos(path string) error {
	devIno, err := util.DevInodeNoFromFilePath(path)
	if err != nil {
		return err
	}
	w.pos.DelOffset(devIno)
	return nil
}

func (w *watcher) Lines() chan *Line {
	return w.lines
}

func (w *watcher) Errors() chan Error {
	return w.errors
}

func (w *watcher) Close() {
	close(w.done)
	<-w.terminated
}
