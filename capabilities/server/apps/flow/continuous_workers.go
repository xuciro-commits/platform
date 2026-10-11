package flow

import (
	"sync"
	"sync/atomic"

	"platformkernel/kernel"
)

// Partition workers for one continuous batch (ADR-0047 §13.3): a declaration
// of concurrency 8 means real partition work, not a number in a declaration.
// One partition is one job, so its state is read and folded by exactly one
// worker at a time; the caller merges the workers' results in the batch's own
// order and still commits one accepted frame. The frame records how many
// workers really overlapped, so occupancy is visible rather than claimed.
type foldPool struct{ limit int }

// run calls job for every index in [0, jobs) with at most the declared number
// of workers at once and returns how many jobs really overlapped. The first
// refusal cancels the run: no worker starts another job, so a batch that
// cannot be folded stops spending work instead of finishing a fold that the
// decision will discard. Everything a worker writes stays in its own slot;
// the merge that follows is single-threaded.
func (p foldPool) run(jobs int, job func(i int, cancelled func() bool) *kernel.Error) (int, *kernel.Error) {
	if jobs == 0 {
		return 0, nil
	}
	var (
		next   = make(chan int)
		done   = make(chan struct{})
		once   sync.Once
		wg     sync.WaitGroup
		active atomic.Int64
		peak   atomic.Int64
		mu     sync.Mutex
		first  *kernel.Error
	)
	limit := min(max(p.limit, 1), jobs)
	stopped := func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
	stop := func(err *kernel.Error) {
		once.Do(func() {
			mu.Lock()
			first = err
			mu.Unlock()
			close(done)
		})
	}
	for range limit {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				if stopped() {
					continue // the run is cancelled: take the slot, do no work
				}
				n := active.Add(1)
				for {
					was := peak.Load()
					if n <= was || peak.CompareAndSwap(was, n) {
						break
					}
				}
				if err := job(i, stopped); err != nil {
					stop(err)
				}
				active.Add(-1)
			}
		}()
	}
	for i := 0; i < jobs && !stopped(); i++ {
		next <- i
	}
	close(next)
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	return int(peak.Load()), first
}
