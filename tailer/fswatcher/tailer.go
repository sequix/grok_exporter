package fswatcher

import (
	"github.com/sirupsen/logrus"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mohae/deepcopy"
	"github.com/sequix/tail"

	"github.com/sequix/grok_exporter/tailer/position"
	"github.com/sequix/grok_exporter/util"
)

// TODO 不掉数据的速率限制（但会增加内存消耗）

// tailer 包装 hpcloud、tail，以 Fan-In 模式将多路输入压入lines chan
// Fan-In模式：https://github.com/tmrts/go-patterns/blob/master/messaging/fan_in.md
type tailer struct {
	*tail.Tail
	stopped     int32
	path        string
	devIno      string
	pos         position.Interface
	outputLines chan *Line
	errors      chan Error
	readAt      atomic.Value
	done        chan bool // deliver whether delete position or not
	terminated  chan struct{}
}

func (w *watcher) newTailer(path string) (*tailer, error) {
	devIno, err := util.DevInodeNoFromFilePath(path)
	if err != nil {
		return nil, err
	}

	fileType, err := util.FileTypeOf(path)
	if err != nil {
		return nil, err
	}

	cfg := deepcopy.Copy(w.tailConfig).(tail.Config)
	cfg.Location.Offset = w.pos.GetOffset(devIno)
	cfg.Logger = w.logger.WithField("path", path)

	// 若使用inotify，在下述场景中，无法拿到该文件的内容
	// 先新建软链接到一个不存在的文件，后建立该文件
	// 为解决上述问题，可以使用轮询方式抓取软链接，常规文件使用inotify
	cfg.Poll = (fileType == util.Symlink)

	w.logger.WithFields(map[string]interface{}{
		"path":     path,
		"fileType": fileType,
		"offset":   cfg.Location.Offset,
	}).Info("new tailer")

	t, err := tail.TailFile(path, cfg)
	if err != nil {
		if w.tailConfig.MustExist && os.IsNotExist(err) {
			return nil, NewErrorf(FileNotFound, err, "file %s not found", err)
		}
		return nil, err
	}

	tailer := &tailer{
		Tail:        t,
		path:        path,
		devIno:      devIno,
		pos:         w.pos,
		outputLines: w.lines,
		errors:      w.errors,
		done:        make(chan bool),
		terminated:  make(chan struct{}),
	}
	tailer.readAt.Store(time.Time{})
	return tailer, nil
}

func (t *tailer) run() {
	for {
		select {
		case event, ok := <-t.Lines:
			if !ok {
				continue
			}
			if event.Err != nil {
				if strings.Contains(event.Err.Error(), "Too much log activity") {
					// 读取速度过快，冷却1s
					continue
				}
				t.errors <- NewStructuredError(event.Err, "reading file", map[string]interface{}{"path": t.path})
				continue
			}
			if len(event.Text) > 0 {
				t.outputLines <- &Line{event.Text, t.Filename}
			}
			offset, err := t.Tail.Tell()
			if err != nil {
				t.errors <- NewStructuredError(event.Err, "updating offset", map[string]interface{}{"path": t.path})
				continue
			}
			t.pos.SetOffset(t.devIno, offset)
			t.readAt.Store(time.Now())
		case delPos := <-t.done:
			t.finalizer(delPos)
			return
		}
	}
}

func (t *tailer) stop(delPos bool) {
	if atomic.CompareAndSwapInt32(&t.stopped, 0, 1) {
		t.done <- delPos
		close(t.done)
		<-t.terminated
	}
}

func (t *tailer) finalizer(delPos bool) {
	t.Logger.(logrus.FieldLogger).WithFields(map[string]interface{}{
		"path":   t.path,
		"devIno": t.devIno,
		"delPos": delPos,
	}).Debug("closing tailer")

	if err := t.Stop(); err != nil {
		t.errors <- NewStructuredError(err, "closing file", map[string]interface{}{"path": t.path})
	}
	if delPos {
		t.pos.DelOffset(t.devIno)
	}
	close(t.terminated)
}
