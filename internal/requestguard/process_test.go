package requestguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCrossProcessRepeatedRequestBlocked(t *testing.T) {
	if os.Getenv("HOVERDICT_GUARD_CHILD") == "yes" {
		g, err := Open(os.Getenv("HOVERDICT_GUARD_PATH"))
		if err != nil {
			t.Fatal(err)
		}
		// Only one independent process is allowed to return success.
		if err := g.Allow("same-word", time.Unix(1795400000, 0)); err == nil {
			os.Exit(0)
		}
		os.Exit(41)
	}
	path := filepath.Join(t.TempDir(), "usage.json")
	var wg sync.WaitGroup
	success := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := exec.Command(os.Args[0], "-test.run=^TestCrossProcessRepeatedRequestBlocked$")
			c.Env = append(os.Environ(), "HOVERDICT_GUARD_CHILD=yes", "HOVERDICT_GUARD_PATH="+path)
			success <- c.Run() == nil
		}()
	}
	wg.Wait()
	close(success)
	allowed := 0
	for v := range success {
		if v {
			allowed++
		}
	}
	if allowed != 1 {
		t.Fatalf("expected 1 allowed free-online request across independent processes, got %d", allowed)
	}
}
