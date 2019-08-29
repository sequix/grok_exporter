package fswatcher

import (
	"fmt"
	"os"

	"github.com/hpcloud/tail"
	"github.com/mohae/deepcopy"

	"github.com/fstab/grok_exporter/tailer/position"
	"github.com/fstab/grok_exporter/util"
)

// tailer 包装 hpcloud.tail，以 Fan-In 模式将多路输入压入lines chan
// Fan-In模式：https://github.com/tmrts/go-patterns/blob/master/messaging/fan_in.md
type tailer struct {
	*tail.Tail
	ino    uint64
	pos    position.Interface
	lines  chan *Line
	errors chan Error
	done   chan struct{}
}

func (w *watcher) newTailer(path string) (*tailer, error) {
	ino, err := util.InodeNoFromFilepath(path)
	if err != nil {
		return nil, err
	}

	cfg := deepcopy.Copy(w.tailConfig).(tail.Config)
	cfg.Location.Offset = w.pos.GetOffset(ino)
	w.logger.Debug(fmt.Sprintf("new file %s at %d", path, cfg.Location.Offset))

	t, err := tail.TailFile(path, cfg)
	if err != nil {
		if w.tailConfig.MustExist && os.IsNotExist(err) {
			return nil, NewErrorf(FileNotFound, err, "file %s not found", err)
		}
		return nil, err
	}

	tailer := &tailer{
		Tail:   t,
		ino:    ino,
		pos:    w.pos,
		lines:  w.lines,
		errors: w.errors,
		done:   make(chan struct{}),
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
				t.errors <- NewErrorf(NotSpecified, event.Err, "reading file %s:", t.Filename)
			}
			if event.Text != "" {
				t.lines <- &Line{
					Line: event.Text,
					File: t.Filename,
				}
			}
			offset, err := t.Tail.Tell()
			if err != nil {
				t.errors <- NewErrorf(NotSpecified, event.Err, "update file offset %s:", t.Filename)
				continue
			}
			t.pos.SetOffset(t.ino, offset)
		case <-t.done:
			if err := t.Stop(); err != nil {
				t.errors <- NewErrorf(NotSpecified, err, "close file %s:", t.Filename)
			}
			return
		}
	}
}

func (t *tailer) stop() {
	close(t.done)
}
