// Package filelock provides a process-shared advisory lock for files in the
// current user's application-data directory. The operating system releases the
// lock when its owning process terminates, even after an unexpected crash.
package filelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var ErrLocked = errors.New("another process holds the lock")

type Lock struct{ file *os.File }

func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	return f.Close()
}

func TryAcquire(path string) (*Lock, error) {
	if path == "" {
		return nil, fmt.Errorf("lock path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	return openExclusive(path)
}

func Acquire(path string) (*Lock, error) {
	deadline := time.Now().Add(15 * time.Second)
	for {
		l, err := TryAcquire(path)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, ErrLocked) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out acquiring %s: %w", filepath.Base(path), err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
