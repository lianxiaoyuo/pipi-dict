package bookmarks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestCrossProcessFavorites(t *testing.T) {
	if os.Getenv("HOVERDICT_BOOKMARK_CHILD") == "yes" {
		path := os.Getenv("HOVERDICT_TEST_PATH")
		word := os.Getenv("HOVERDICT_TEST_WORD")
		_, err := Toggle(path, map[string]bool{}, word)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "bookmarks.json")
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestCrossProcessFavorites$")
			cmd.Env = append(os.Environ(), "HOVERDICT_BOOKMARK_CHILD=yes", "HOVERDICT_TEST_PATH="+path, fmt.Sprintf("HOVERDICT_TEST_WORD=word%d", i))
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("%v: %s", err, out)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	got, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 12 {
		t.Fatalf("cross-process lost %d writes, got %d", 12-len(got), len(got))
	}
}
