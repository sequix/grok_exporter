package tailer

import (
	"container/list"
	"io"
	"sync"

	"github.com/sequix/grok_exporter/tailer/fswatcher"
)

// lineBuffer is a thread safe queue for *fswatcher.Line.
type lineBuffer interface {
	io.Closer // will interrupt BlockingPop()
	Push(line *fswatcher.Line)
	BlockingPop() *fswatcher.Line // can be interrupted by calling Close()
	Len() int
	Clear()
}

func NewLineBuffer() lineBuffer {
	return &lineBufferImpl{
		buffer: list.New(),
		lock:   sync.NewCond(&sync.Mutex{}),
		closed: false,
	}
}

type lineBufferImpl struct {
	buffer *list.List
	lock   *sync.Cond
	closed bool
}

func (b *lineBufferImpl) Push(line *fswatcher.Line) {
	b.lock.L.Lock()
	defer b.lock.L.Unlock()
	if !b.closed {
		b.buffer.PushBack(line)
		b.lock.Signal()
	}
}

// Interrupted by Close(), returns nil when Close() is called.
func (b *lineBufferImpl) BlockingPop() *fswatcher.Line {
	b.lock.L.Lock()
	defer b.lock.L.Unlock()
	if !b.closed {
		for b.buffer.Len() == 0 && !b.closed {
			b.lock.Wait()
		}
		if !b.closed {
			first := b.buffer.Front()
			b.buffer.Remove(first)
			return first.Value.(*fswatcher.Line)
		}
	}
	return nil
}

func (b *lineBufferImpl) Close() error {
	b.lock.L.Lock()
	defer b.lock.L.Unlock()
	if !b.closed {
		b.closed = true
		b.lock.Signal()
	}
	return nil
}

func (b *lineBufferImpl) Len() int {
	b.lock.L.Lock()
	defer b.lock.L.Unlock()
	return b.buffer.Len()
}

func (b *lineBufferImpl) Clear() {
	b.lock.L.Lock()
	defer b.lock.L.Unlock()
	b.buffer = list.New()
}
