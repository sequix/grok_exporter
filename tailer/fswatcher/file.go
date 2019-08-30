package fswatcher

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sequix/grok_exporter/tailer/position"
	"github.com/sequix/grok_exporter/util"
)

type file struct {
	*os.File
	*bufio.Reader
	lines      chan *Line
	errors     chan Error
	pos        position.Interface
	devIno     string
	path       string
	done       chan struct{}
	terminated chan struct{}
}

func (p *poller) newFile(path string) (*file, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	devIno, err := util.DevInodeNoFromFilePath(path)
	if err != nil {
		return nil, err
	}

	offset := p.pos.GetOffset(devIno)
	p.logger.Debug(fmt.Sprintf("new file %s at %d", path, offset))

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	return &file{
		lines:      p.lines,
		errors:     p.errors,
		devIno:     devIno,
		path:       path,
		pos:        p.pos,
		File:       f,
		Reader:     bufio.NewReader(f),
		done:       make(chan struct{}),
		terminated: make(chan struct{}),
	}, nil
}

func (f *file) run() {
	defer f.finalize()
	for {
		line, err := f.readline()
		if err != nil {
			if err == io.EOF {
				return
			}
			select {
			case f.errors <- NewErrorf(NotSpecified, err, "reading file %s", f.path):
			case <-f.done:
				return
			}
		} else {
			select {
			case f.lines <- line:
			case <-f.done:
				return
			}
		}
	}
}

func (f *file) finalize() {
	if err := f.Close(); err != nil {
		f.errors <- NewErrorf(NotSpecified, err, "close file %s", f.path)
	}
	close(f.terminated)
}

func (f *file) stop() {
	close(f.done)
	<-f.terminated
}

// 不支持mac换行符\r
func (f *file) readline() (*Line, error) {
	lineStr, err := f.ReadString('\n')
	if err != nil {
		return nil, err
	}

	// 更新文件偏移
	offset, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	f.pos.SetOffset(f.devIno, offset)

	return &Line{
		Line: strings.TrimRight(lineStr, "\r\n"),
		File: f.path,
	}, nil
}
