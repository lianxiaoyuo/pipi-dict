//go:build windows

package filelock

import (
	"errors"
	"os"
	"syscall"
)

// FILE_SHARE_NONE is essential: a second Windows process cannot open this
// file until the first closes it (including on unexpected process exit).
func openExclusive(path string) (*Lock, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, syscall.Errno(32)) || errors.Is(err, syscall.Errno(33)) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return &Lock{file: os.NewFile(uintptr(h), path)}, nil
}
