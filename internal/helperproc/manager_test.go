package helperproc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHelperChild(t *testing.T) {
	if os.Getenv("PIPIDICT_TEST_HELPER") != "yes" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		value := scanner.Text()
		if value == "hang" {
			time.Sleep(time.Hour)
		}
		if value == "exit" {
			os.Exit(9)
		}
		fmt.Println(value)
	}
	os.Exit(0)
}
func newTestManager(maxUses int) *Manager {
	return New(func() *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperChild$")
		cmd.Env = append(os.Environ(), "PIPIDICT_TEST_HELPER=yes")
		return cmd
	}, maxUses)
}
func TestRepeatedRequestsAndReuse(t *testing.T) {
	m := newTestManager(41)
	defer m.Close()
	for i := 0; i < 240; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		want := fmt.Sprintf("word-%d", i)
		got, err := m.Request(ctx, want)
		cancel()
		if err != nil || got != want {
			t.Fatalf("iteration %d, got %q error %v", i, got, err)
		}
	}
}
func TestHangCancelledAndNextRequestRecovered(t *testing.T) {
	m := newTestManager(30)
	defer m.Close()
	for i := 0; i < 16; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, err := m.Request(ctx, "hang")
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("hang %d: %v", i, err)
		}
		ctx2, c2 := context.WithTimeout(context.Background(), time.Second)
		got, err := m.Request(ctx2, "recovered")
		c2()
		if err != nil || got != "recovered" {
			t.Fatalf("recovery %d, got %q error %v", i, got, err)
		}
	}
}
func TestConcurrentRequestsSerialized(t *testing.T) {
	m := newTestManager(30)
	defer m.Close()
	var wg sync.WaitGroup
	problems := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			want := fmt.Sprintf("parallel-%d", i)
			got, err := m.Request(ctx, want)
			if err != nil {
				problems <- err
			} else if got != want {
				problems <- fmt.Errorf("want %s, got %s", want, got)
			}
		}(i)
	}
	wg.Wait()
	close(problems)
	for e := range problems {
		if !strings.Contains(e.Error(), "context canceled") {
			t.Fatal(e)
		}
	}
}

func TestCrashedProcessRestarts(t *testing.T) {
	m := newTestManager(100)
	defer m.Close()
	for i := 0; i < 12; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
		_, err := m.Request(ctx, "exit")
		cancel()
		if err == nil {
			t.Fatalf("expected helper crash error on %d", i)
		}
		ctx2, stop := context.WithTimeout(context.Background(), time.Second)
		got, err := m.Request(ctx2, "healthy")
		stop()
		if err != nil || got != "healthy" {
			t.Fatalf("restarted %d: got %q err %v", i, got, err)
		}
	}
}
