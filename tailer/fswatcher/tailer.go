package fswatcher

import (
	"github.com/hpcloud/tail"
)

// tailer 包装 hpcloud.tail，以 Fan-In 模式将多路输入压入lines chan
// Fan-In模式：https://github.com/tmrts/go-patterns/blob/master/messaging/fan_in.md
type tailer struct {
	*tail.Tail
	lines  chan *Line
	errors chan Error
	done   chan struct{}
}

func (w *watcher) newTailer(path string) (*tailer, error) {
	t, err := tail.TailFile(path, w.tailConfig)
	if err != nil {
		return nil, err
	}

	tailer := &tailer{
		Tail:   t,
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
