package automation

import (
	"bytes"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type WindowState struct {
	Rows      []Signal  `json:"rows"`
	EmittedAt time.Time `json:"emittedAt,omitzero"`
	Ready     bool      `json:"ready"`
	Duplicate int       `json:"duplicate"`
}

func checkWindow(w platform.StreamWindow) error {
	if w.Node == "" || w.WindowMS < 100 || w.WindowMS > 3600000 || w.SlideMS < 1 || w.SlideMS > w.WindowMS || w.WatermarkMS < 0 || w.WatermarkMS > 600000 || w.MaxRecords < 1 || w.MaxRecords > 1000000 || !slices.Contains([]string{"sideOutput", "accept", "reject"}, w.LateEvents) {
		return fmt.Errorf("a stream window needs a node, bounded event-time periods, row budget and late-event policy")
	}
	return nil
}

func foldWindow(frame *BatchFrame, declared platform.Continuous, batch Batch, now time.Time) (folded, rejected int, refusal *kernel.Error) {
	w := *declared.Window
	if err := checkWindow(w); err != nil {
		return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	state := WindowState{Rows: []Signal{}}
	if raw := frame.State[w.Node]; len(raw) > 0 {
		if json.Unmarshal(raw, &state) != nil || state.Rows == nil {
			return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The saved event-time window cannot be decoded")
		}
	}
	watermark := maxTime(frame.Watermark, now)
	for _, signal := range batch.Signals {
		watermark = maxTime(watermark, signal.At)
	}
	boundary := windowBoundary(w, watermark, now)
	retentionBoundary := boundary
	if !boundary.After(state.EmittedAt) {
		retentionBoundary = state.EmittedAt.Add(time.Duration(w.SlideMS) * time.Millisecond)
		if w.LateEvents == "accept" {
			retentionBoundary = state.EmittedAt
		}
	}
	oldest := retentionBoundary.Add(-time.Duration(w.WindowMS) * time.Millisecond)
	seen := map[signalIdentity]Signal{}
	for _, row := range state.Rows {
		seen[windowSignalKey(row)] = row
	}
	added := map[signalIdentity]bool{}
	state.Rows = slices.DeleteFunc(slices.Clone(state.Rows), func(s Signal) bool { return s.At.Before(oldest) })
	appendDead := func(signal Signal, reason string) {
		frame.DeadLetters = append(frame.DeadLetters, DeadLetter{Batch: batch.ID, Partition: signal.Partition, Key: signal.Key, At: signal.At, RecordedAt: now, Reason: reason, Value: string(signal.Value)})
		rejected++
	}
	lateAccepted := false
	for _, signal := range batch.Signals {
		if signal.At.IsZero() || signal.Key == "" {
			appendDead(signal, "missing event identity or time")
			continue
		}
		key := windowSignalKey(signal)
		if prior, exists := seen[key]; exists {
			if !prior.At.Equal(signal.At) || !bytes.Equal(prior.Value, signal.Value) {
				return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The event identity was reused with different content")
			}
			state.Duplicate++
			continue
		}
		seen[key] = signal
		if signal.At.Before(oldest) {
			switch w.LateEvents {
			case "reject":
				return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "An event is outside the declared window and watermark grace")
			case "sideOutput":
				appendDead(signal, "late: outside the event-time window and watermark grace")
				continue
			case "accept":
				lateAccepted = true
			}
		}
		signal.Value = slices.Clone(signal.Value)
		state.Rows = append(state.Rows, signal)
		added[key] = true
		folded++
	}
	slices.SortFunc(state.Rows, func(a, b Signal) int {
		if n := a.At.Compare(b.At); n != 0 {
			return n
		}
		if a.Partition != b.Partition {
			if a.Partition < b.Partition {
				return -1
			}
			return 1
		}
		if a.Key < b.Key {
			return -1
		}
		if a.Key > b.Key {
			return 1
		}
		return 0
	})
	if overflow := len(state.Rows) - w.MaxRecords; overflow > 0 {
		for _, signal := range state.Rows[:overflow] {
			appendDead(signal, "overflow: event-time window row budget")
			if added[windowSignalKey(signal)] {
				folded--
			}
		}
		state.Rows = slices.Clone(state.Rows[overflow:])
	}
	pruneDeadLetters(frame, declared, now)
	newSlide := state.EmittedAt.IsZero() || boundary.After(state.EmittedAt)
	state.Ready = false
	if newSlide {
		state.EmittedAt = boundary
		state.Ready = len(windowSignals(state, w)) > 0 || declared.Entry != "" || declared.Aggregate != nil
	} else if lateAccepted {
		state.Ready = true
	}
	raw, err := json.Marshal(state)
	if err != nil || declared.State > 0 && len(raw) > declared.State {
		return 0, 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The event-time window exceeds its declared state budget")
	}
	frame.State[w.Node], frame.Watermark = raw, watermark
	return folded, rejected, nil
}

func windowBoundary(w platform.StreamWindow, watermark, now time.Time) time.Time {
	high := maxTime(watermark, now)
	grace := time.Duration(w.WatermarkMS) * time.Millisecond
	return high.Add(-grace).UTC().Truncate(time.Duration(w.SlideMS) * time.Millisecond)
}

func windowSignals(state WindowState, w platform.StreamWindow) []Signal {
	if state.EmittedAt.IsZero() {
		return nil
	}
	start := state.EmittedAt.Add(-time.Duration(w.WindowMS) * time.Millisecond)
	rows := make([]Signal, 0, len(state.Rows))
	for _, signal := range state.Rows {
		if (w.LateEvents == "accept" || !signal.At.Before(start)) && signal.At.Before(state.EmittedAt) {
			rows = append(rows, signal)
		}
	}
	return rows
}

type signalIdentity struct{ partition, key string }

func windowSignalKey(signal Signal) signalIdentity {
	return signalIdentity{signal.Partition, signal.Key}
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

type foldPool struct{ limit int }

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
					continue
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
