package fastclick

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestImmediateFirstClickNoDebounce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clicks := make(chan int, 1)
	started := make(chan int, 1)
	displayed := make(chan int, 1)
	go Run(ctx, clicks, func(_ context.Context, n int) int { started <- n; return n }, func(_ int, n int) { displayed <- n })
	clicks <- 7
	select {
	case n := <-started:
		if n != 7 {
			t.Fatalf("started %d", n)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("first lookup delayed")
	}
	select {
	case n := <-displayed:
		if n != 7 {
			t.Fatalf("delivered %d", n)
		}
	case <-time.After(time.Second):
		t.Fatal("no result")
	}
}

func TestBurstDoesNotCancelOrRestartActiveHelper(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clicks := make(chan int, 256)
	begun := make(chan int, 3)
	release := make(chan struct{})
	displayed := make(chan int, 5)
	var calls atomic.Int32
	go Run(ctx, clicks, func(_ context.Context, n int) int {
		calls.Add(1)
		begun <- n
		if n == 0 {
			<-release
		}
		return n
	}, func(_ int, n int) { displayed <- n })
	clicks <- 0
	select {
	case n := <-begun:
		if n != 0 {
			t.Fatal(n)
		}
	case <-time.After(time.Second):
		t.Fatal("first not started")
	}
	for i := 1; i <= 100; i++ {
		clicks <- i
	}
	// The old helper must be allowed to return normally, not canceled/killed.
	close(release)
	select {
	case n := <-displayed:
		if n != 100 {
			t.Fatalf("obsolete result %d", n)
		}
	case <-time.After(time.Second):
		t.Fatal("latest not delivered")
	}
	if calls.Load() != 2 {
		t.Fatalf("wanted first + latest only, got %d", calls.Load())
	}
}

func TestRepeatedDeadlinesDoNotPoisonNextLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clicks := make(chan int, 16)
	displayed := make(chan int, 16)
	go Run(ctx, clicks, func(_ context.Context, n int) int {
		if n < 6 {
			time.Sleep(4 * time.Millisecond)
		} // simulate a helper timeout and recovery
		return n
	}, func(_ int, n int) { displayed <- n })
	for i := 0; i < 8; i++ {
		clicks <- i
		time.Sleep(7 * time.Millisecond)
	}
	select {
	case n := <-displayed:
		if n < 0 || n > 7 {
			t.Fatal(n)
		}
	case <-time.After(time.Second):
		t.Fatal("stuck")
	}
	clicks <- 99
	deadline := time.After(time.Second)
	for {
		select {
		case n := <-displayed:
			if n == 99 {
				return
			}
		case <-deadline:
			t.Fatal("did not recover")
		}
	}
}

func TestCancelOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clicks := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		Run(ctx, clicks, func(ctx context.Context, n int) int { <-ctx.Done(); return n }, func(_ int, _ int) {})
		close(done)
	}()
	clicks <- 1
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
}

// A burst should not build a backlog of COM operations. A slow in-flight
// operation plus a single latest queued operation is the strict upper bound.
func Test250RapidSwitchesKeepOnlyLatest(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	requests := make(chan int, 300)
	begun := make(chan int, 2)
	release := make(chan struct{})
	results := make(chan int, 4)
	var executed atomic.Int32
	go Run(ctx, requests, func(_ context.Context, n int) int {
		executed.Add(1)
		if n == 0 {
			begun <- n
			<-release
		}
		return n
	}, func(_ int, n int) { results <- n })
	requests <- 0
	select {
	case <-begun:
	case <-time.After(time.Second):
		t.Fatal("first did not start")
	}
	for i := 1; i <= 250; i++ {
		requests <- i
	}
	close(release)
	select {
	case n := <-results:
		if n != 250 {
			t.Fatalf("delivered %d, want 250", n)
		}
	case <-time.After(time.Second):
		t.Fatal("latest click did not finish")
	}
	if calls := executed.Load(); calls != 2 {
		t.Fatalf("executed %d, expected first and last only", calls)
	}
}

func Test250SequentialClicksNoPermanentStall(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	requests := make(chan int, 1)
	results := make(chan int, 1)
	go Run(ctx, requests, func(_ context.Context, n int) int { return n }, func(_ int, n int) { results <- n })
	for i := 0; i < 250; i++ {
		requests <- i
		select {
		case n := <-results:
			if n != i {
				t.Fatalf("iteration %d: returned %d", i, n)
			}
		case <-time.After(time.Second):
			t.Fatalf("iteration %d stalled", i)
		}
	}
}
