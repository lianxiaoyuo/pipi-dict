package fastclick

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// A second click must finish even if the first UI Automation provider is hung.
func TestDualSlowFirstDoesNotBlockSecond(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	requests := make(chan int, 4)
	entered := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan int, 4)
	var active, peak atomic.Int32
	go RunDual(ctx, requests, func(_ context.Context, lane int, n int) int {
		v := active.Add(1)
		for {
			old := peak.Load()
			if v <= old || peak.CompareAndSwap(old, v) {
				break
			}
		}
		defer active.Add(-1)
		if n == 1 {
			close(entered)
			<-release
		}
		return n
	}, func(_ int, n int) { delivered <- n })
	requests <- 1
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first not started")
	}
	requests <- 2
	select {
	case got := <-delivered:
		if got != 2 {
			t.Fatalf("got stale %d", got)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("second click blocked behind hung first")
	}
	close(release)
	if peak.Load() != 2 {
		t.Fatalf("expected two parallel lanes, got %d", peak.Load())
	}
}

func TestDual200ClicksBoundedAndLatestOnly(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	requests := make(chan int, 256)
	begun := make(chan int, 3)
	release := make(chan struct{})
	delivered := make(chan int, 4)
	var running, peak, calls atomic.Int32
	go RunDual(ctx, requests, func(_ context.Context, lane int, n int) int {
		calls.Add(1)
		active := running.Add(1)
		for {
			v := peak.Load()
			if v >= active || peak.CompareAndSwap(v, active) {
				break
			}
		}
		defer running.Add(-1)
		if n == 1 || n == 2 {
			begun <- n
			<-release
		}
		return n
	}, func(_ int, n int) { delivered <- n })
	requests <- 1
	select {
	case <-begun:
	case <-time.After(time.Second):
		t.Fatal("first not running")
	}
	requests <- 2
	select {
	case <-begun:
	case <-time.After(time.Second):
		t.Fatal("second not running")
	}
	for n := 3; n <= 200; n++ {
		requests <- n
	}
	close(release)
	select {
	case n := <-delivered:
		if n != 200 {
			t.Fatalf("displayed obsolete %d", n)
		}
	case <-time.After(time.Second):
		t.Fatal("latest click never completed")
	}
	if peak.Load() > 2 {
		t.Fatalf("%d workers running", peak.Load())
	}
	if calls.Load() != 3 {
		t.Fatalf("expected first, second, latest only: %d", calls.Load())
	}
}

func TestDualSequential500(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	in := make(chan int, 1)
	out := make(chan int, 1)
	go RunDual(ctx, in, func(_ context.Context, _ int, n int) int { return n }, func(_ int, n int) { out <- n })
	for n := 0; n < 500; n++ {
		in <- n
		select {
		case got := <-out:
			if got != n {
				t.Fatalf("%d != %d", got, n)
			}
		case <-time.After(time.Second):
			t.Fatalf("stalled at %d", n)
		}
	}
}
