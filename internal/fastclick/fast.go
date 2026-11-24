// Package fastclick schedules click-to-lookup work without restarting the
// accessibility provider on every new click. At most one request executes,
// with just the newest pending request retained while it is busy.
package fastclick

import "context"

// Run is an immediate, latest-pending scheduler. New clicks NEVER cancel an
// in-flight request (which could destroy a reusable COM helper). Its response
// is ignored if superseded. The caller must apply a finite per-query timeout
// so a faulty provider cannot monopolize the single worker indefinitely.
// Deliver is invoked on the scheduling goroutine, never concurrently.
func Run[T any, R any](ctx context.Context, requests <-chan T,
	execute func(context.Context, T) R, deliver func(T, R)) {
	type outcome struct {
		seq   uint64
		input T
		value R
	}
	finished := make(chan outcome, 1)
	var pending T
	var seq uint64
	hasPending, active := false, false

	start := func(value T, n uint64) {
		active = true
		go func() {
			result := execute(ctx, value)
			select {
			case finished <- outcome{seq: n, input: value, value: result}:
			case <-ctx.Done():
			}
		}()
	}

	// Drain accumulated clicks before dispatching or displaying a result.
	// Otherwise, a completed slow call might flash an obsolete definition while
	// the newest mouse event is already waiting in the channel.
	drain := func() bool {
		for {
			select {
			case next, ok := <-requests:
				if !ok {
					return false
				}
				seq++
				pending = next
				hasPending = true
			default:
				return true
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case next, ok := <-requests:
			if !ok {
				return
			}
			seq++
			pending, hasPending = next, true
			if !drain() {
				return
			}
			if !active {
				start(pending, seq)
				hasPending = false
			}
		case done := <-finished:
			active = false
			if !drain() {
				return
			}
			if done.seq == seq && !hasPending && ctx.Err() == nil {
				deliver(done.input, done.value)
			}
			if hasPending && ctx.Err() == nil {
				start(pending, seq)
				hasPending = false
			}
		}
	}
}
