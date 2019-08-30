package util

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func DevInodeNoFromFileInfo(fi os.FileInfo) (string, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New(fmt.Sprintf("%s: cannot get *syscall.Stat_t", fi.Name()))
	}
	return fmt.Sprintf("%x-%x", st.Dev, st.Ino), nil
}

func DevInodeNoFromFile(file *os.File) (string, error) {
	fi, err := file.Stat()
	if err != nil {
		return "", err
	}
	return DevInodeNoFromFileInfo(fi)
}

func DevInodeNoFromFilePath(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return DevInodeNoFromFileInfo(fi)
}

