package flow

import (
	"context"
	"errors"
	"math"
	"testing"
)

// Rounding happens at the original f32 input/output boundary, not on every
// addition. The kernel sums f32 inputs in f64 before writing the five f32s.
func TestTelemetryKernelPreservesPackedF32Semantics(t *testing.T) {
	active := func() bool { return false }
	got, err := windowF32Stats([]float64{16_777_217, 1, -16_777_216}, 0, active)
	if err != nil {
		t.Fatal(err)
	}
	if got["count"] != 3 || got["sum"] != 1 || got["mean"] != float64(float32(1.0/3)) || got["above"] != 2 || got["min"] != -16_777_216 || got["max"] != 16_777_216 {
		t.Fatalf("original f32 ABI differs: %v", got)
	}
	empty, err := windowF32Stats(nil, 11.5, active)
	if err != nil || empty["count"] != 0 || empty["mean"] != 0 || empty["min"] != 0 || empty["max"] != 0 {
		t.Fatalf("empty kernel result: %v %v", empty, err)
	}
	if _, err := windowF32Stats([]float64{math.Inf(1)}, 11.5, active); err == nil {
		t.Fatal("invalid f32 value accepted")
	}
}

func TestTelemetryKernelPreservesF32Boundaries(t *testing.T) {
	active := func() bool { return false }
	step := float64(math.Nextafter32(1, 2)) - 1
	got, err := windowF32Stats([]float64{1 + step}, 1+step*0.75, active)
	if err != nil || got["above"] != 0 {
		t.Fatalf("threshold did not round to f32 before comparison: %v %v", got, err)
	}
	got, err = windowF32Stats([]float64{math.Copysign(0, -1), 0}, math.MaxFloat64, active)
	if err != nil || !math.Signbit(got["min"]) || !math.Signbit(got["max"]) || got["above"] != 0 {
		t.Fatalf("signed-zero ties or finite threshold conversion differ: %v %v", got, err)
	}
	for _, values := range [][]float64{{math.NaN()}, {math.MaxFloat64}, {math.MaxFloat32, math.MaxFloat32}} {
		if _, err := windowF32Stats(values, 0, active); err == nil {
			t.Fatalf("non-finite f32 input or output accepted: %v", values)
		}
	}
}

func TestTelemetryKernelStopsCancelledPreparation(t *testing.T) {
	if _, err := windowF32Stats([]float64{1}, 0, func() bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preparation continued: %v", err)
	}
}
