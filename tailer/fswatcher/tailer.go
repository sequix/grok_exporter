package fswatcher

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hpcloud/tail"
	"github.com/mohae/deepcopy"

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

	cfg := deepcopy.Copy(w.tailConfig).(tail.Config)
	cfg.Location.Offset = w.pos.GetOffset(devIno)
	w.logger.Infof(fmt.Sprintf("tailing new file %s at %d", path, cfg.Location.Offset))

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
				select {
				case t.errors <- NewErrorf(NotSpecified, event.Err, "reading file %s:", t.Filename):
				case <-t.done:
					return
				}
			}
			if len(event.Text) > 0 {
				select {
				case t.outputLines <- &Line{event.Text, t.Filename}:
				case <-t.done:
					return
				}
			}
			offset, err := t.Tail.Tell()
			if err != nil {
				select {
				case t.errors <- NewErrorf(NotSpecified, event.Err, "update file offset %s:", t.Filename):
				case <-t.done:
					return
				}
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
	if err := t.Stop(); err != nil {
		t.errors <- NewErrorf(NotSpecified, err, "close file %s:", t.Filename)
	}
	if delPos {
		t.pos.DelOffset(t.devIno)
	}
	close(t.terminated)
}
