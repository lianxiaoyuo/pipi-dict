package requestguard

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestTwoInstancesRespectSameLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "online_usage.json")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := a.Allow("together", now); err != nil {
		t.Fatal(err)
	}
	if err := b.Allow("together", now.Add(time.Second)); err == nil {
		t.Fatal("second process bypassed repeat limit")
	}
}
func TestConcurrentInstancesCannotOverrunBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	now := time.Now()
	var wg sync.WaitGroup
	success := make(chan bool, HourlyLimit+20)
	for i := 0; i < HourlyLimit+20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g, err := Open(path)
			if err == nil {
				err = g.Allow(fmt.Sprintf("word-%d", i), now)
			}
			success <- err == nil
		}(i)
	}
	wg.Wait()
	close(success)
	ok := 0
	for successQuery := range success {
		if successQuery {
			ok++
		}
	}
	if ok != HourlyLimit {
		t.Fatalf("expected exactly %d permitted requests, got %d", HourlyLimit, ok)
	}
	entries, err := readEntries(path)
	if err != nil || len(entries) != HourlyLimit {
		t.Fatalf("stored ledger: %v entries=%d", err, len(entries))
	}
}
