package util

import (
	"errors"
	"fmt"
	"github.com/sequix/grok_exporter/tailer/glob"
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

type FileType string

var (
	Regular    FileType = "regular"
	Directory  FileType = "directory"
	Symlink    FileType = "symlink"
	Device     FileType = "device"
	CharDevice FileType = "charDevice"
	NamedPipe  FileType = "fifo"
	Socket     FileType = "socket"
	Temporary  FileType = "temporary "
)

func FileTypeOf(path string) (FileType, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	mode := fi.Mode()
	switch {
	case (mode & os.ModeSymlink) != 0:
		return Symlink, nil
	case (mode & os.ModeDir) != 0:
		return Directory, nil
	case (mode & os.ModeDevice) != 0:
		return Device, nil
	case (mode & os.ModeCharDevice) != 0:
		return CharDevice, nil
	case (mode & os.ModeNamedPipe) != 0:
		return NamedPipe, nil
	case (mode & os.ModeSocket) != 0:
		return Socket, nil
	case (mode & os.ModeTemporary) != 0:
		return Temporary, nil
	}
	return Regular, nil
}

func MatchGlobs(path string, globs []glob.Glob) bool {
	for i := range globs {
		if globs[i].Match(path) {
			return true
		}
	}
	return false
}
