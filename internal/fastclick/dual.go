package fastclick

import "context"

// RunDual uses exactly two independent execution lanes. A slow in-flight
// accessibility call cannot block the newest click as long as another lane
// is available. Excess clicks are coalesced into one latest pending request;
// there is never an unbounded queue of slow COM calls or child processes.
// Each lane must own its own helper process/COM apartment, not share a lock.
// A result is delivered only if it is still the newest observed request.
func RunDual[T any, R any](ctx context.Context, requests <-chan T,
	execute func(context.Context, int, T) R, deliver func(T, R)) {
	type outcome struct {
		lane  int
		seq   uint64
		input T
		value R
	}
	finished := make(chan outcome, 2)
	var busy [2]bool
	var pending T
	var seq uint64
	hasPending := false

	startPending := func() {
		if !hasPending || ctx.Err() != nil {
			return
		}
		for lane := range busy {
			if busy[lane] {
				continue
			}
			busy[lane] = true
			value, n := pending, seq
			hasPending = false
			go func(lane int, value T, n uint64) {
				result := execute(ctx, lane, value)
				select {
				case finished <- outcome{lane: lane, seq: n, input: value, value: result}:
				case <-ctx.Done():
				}
			}(lane, value, n)
			return
		}
	}
	drain := func() bool {
		for {
			select {
			case next, ok := <-requests:
				if !ok {
					return false
				}
				seq++
				pending, hasPending = next, true
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
			startPending()
		case done := <-finished:
			busy[done.lane] = false
			if !drain() {
				return
			}
			if done.seq == seq && !hasPending && ctx.Err() == nil {
				deliver(done.input, done.value)
			}
			startPending()
		}
	}
}
