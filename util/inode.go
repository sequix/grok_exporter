package util

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func InodeNoFromFileInfo(fi os.FileInfo) (uint64, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New(fmt.Sprintf("%s: cannot get *syscall.Stat_t", fi.Name()))
	}
	return st.Ino, nil
}

func InodeNoFromFile(file *os.File) (uint64, error) {
	fi, err := file.Stat()
	if err != nil {
		return 0, err
	}
	return InodeNoFromFileInfo(fi)
}

func InodeNoFromFilepath(path string) (uint64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return InodeNoFromFileInfo(fi)
}