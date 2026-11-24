package filelock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestExclusiveAndCrashSafeRelease(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.lock")
	l, err := TryAcquire(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = TryAcquire(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock should fail: %v", err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	l2, err := TryAcquire(p)
	if err != nil {
		t.Fatal(err)
	}
	l2.Close()
}
