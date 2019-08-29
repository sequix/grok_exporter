package fswatcher

import (
	"bufio"
	"os"
	"strings"
)

type file struct {
	*os.File
	*bufio.Reader
	path string
}

func newFile(path string) (*file, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &file{
		path:   path,
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
	return &Line{
		Line: result.String(),
		File: f.path,
	}, nil
}
