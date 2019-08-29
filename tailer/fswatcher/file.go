package fswatcher

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fstab/grok_exporter/tailer/position"
	"github.com/fstab/grok_exporter/util"
)

type file struct {
	*os.File
	*bufio.Reader
	pos  position.Interface
	ino  uint64
	path string
}

func (p *poller) newFile(path string) (*file, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	ino, err := util.InodeNoFromFilepath(path)
	if err != nil {
		return nil, err
	}

	offset := p.pos.GetOffset(ino)
	p.logger.Debug(fmt.Sprintf("new file %s at %d", path, offset))

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	return &file{
		ino:    ino,
		path:   path,
		pos:    p.pos,
		File:   f,
		Reader: bufio.NewReader(f),
	}, err
}

func (f *file) readline() (*Line, error) {
	// builder 内部使用 []byte 和 unsafe.Pointer 避免内存分配和类型转换
	result := strings.Builder{}
	for {
		// bufio.Reader.ReadLine会利用reader的缓存，大小通常是文件系统的块大小
		partial, isPrefix, err := f.ReadLine()
		if err != nil {
			return nil, err
		}
		result.Write(partial)
		if !isPrefix {
			break
		}
	}

	// 更新文件偏移
	offset, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	f.pos.SetOffset(f.ino, offset)

	return &Line{
		Line: result.String(),
		File: f.path,
	}, nil
}
