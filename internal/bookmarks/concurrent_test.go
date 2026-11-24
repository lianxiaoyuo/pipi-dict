package bookmarks

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// Emulates 36 independently started instances with stale, inconsistent
// in-memory snapshots. Every mutation must merge against the latest disk state.
func TestConcurrentStaleSnapshotNoLostWords(t *testing.T) {
	p := filepath.Join(t.TempDir(), "saved.json")
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 36; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Toggle(p, map[string]bool{}, fmt.Sprintf("word%d", i))
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	words, _, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(words) != 36 {
		t.Fatalf("lost writes: only %d of 36 words survived: %v", len(words), words)
	}
}
func TestStaleSnapshotDeletePreservesOtherWords(t *testing.T) {
	p := filepath.Join(t.TempDir(), "saved.json")
	first, err := Toggle(p, map[string]bool{}, "apple")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Toggle(p, map[string]bool{}, "banana")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Delete(p, first, "apple")
	if err != nil {
		t.Fatal(err)
	}
	words, _, err := Load(p)
	if err != nil || words["apple"] || !words["banana"] || len(words) != 1 {
		t.Fatalf("stale delete lost words: %v %v", words, err)
	}
}
