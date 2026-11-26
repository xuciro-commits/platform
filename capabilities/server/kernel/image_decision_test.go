package kernel

import (
	"context"
	"testing"

	"platformserver/platform"
)

func TestImageDecisionExpensiveAttemptFallsBackUnderSameLock(t *testing.T) {
	for _, attempts := range []int{0, 1} {
		t.Run(map[int]string{0: "default", 1: "expensive"}[attempts], func(t *testing.T) {
			var reads, holds, unlocks, direct, finishes int
			held := false
			r := ImageDecision[int, int]{
				Attempts: attempts,
				Stopped:  func() bool { return false }, Quarantined: func() bool { return false },
				Hold: func() error {
					if held {
						t.Fatal("double hold")
					}
					held = true
					holds++
					return nil
				},
				Unlock: func() {
					if !held {
						t.Fatal("unlock without hold")
					}
					held = false
					unlocks++
				},
				Contended: func() bool { return false }, Unavailable: func() bool { return false },
				Load: func() (func() (int, int, *platform.Error, error), func(int) bool) {
					return func() (int, int, *platform.Error, error) {
							if held {
								t.Fatal("optimistic read held lane")
							}
							reads++
							return 1, 1, nil, nil
						}, func(int) bool {
							if !held {
								t.Fatal("match outside lane")
							}
							return false
						}
				},
				Direct: func() (int, int, *platform.Error, error) {
					if !held {
						t.Fatal("fallback outside lane")
					}
					direct++
					return 2, 2, nil, nil
				},
				Finish: func(decision, value int, refusal *platform.Error) (int, *platform.Error) {
					if !held || decision != 2 {
						t.Fatal("finished stale decision")
					}
					finishes++
					return value, refusal
				},
				Count: func(string) {},
			}
			value, refusal := r.Run(context.Background())
			wantReads, wantHolds := 3, 4
			if attempts == 1 {
				wantReads, wantHolds = 1, 1
			}
			if value != 2 || refusal != nil || reads != wantReads || holds != wantHolds || unlocks != holds || direct != 1 || finishes != 1 || held {
				t.Fatalf("value=%d refusal=%v reads=%d holds=%d unlocks=%d direct=%d finishes=%d held=%v", value, refusal, reads, holds, unlocks, direct, finishes, held)
			}
		})
	}
}
