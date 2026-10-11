package flow

import (
	"math"
	"testing"
)

// Rounding happens at the original f32 input/output boundary, not on every
// addition. The kernel sums f32 inputs in f64 before writing the five f32s.
func TestTelemetryKernelPreservesPackedF32Semantics(t *testing.T) {
	got, err := windowF32Stats([]float64{16_777_217, 1, -16_777_216}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got["count"] != 3 || got["sum"] != 1 || got["mean"] != float64(float32(1.0/3)) || got["above"] != 2 || got["min"] != -16_777_216 || got["max"] != 16_777_216 {
		t.Fatalf("original f32 ABI differs: %v", got)
	}
	empty, err := windowF32Stats(nil, 11.5)
	if err != nil || empty["count"] != 0 || empty["mean"] != 0 || empty["min"] != 0 || empty["max"] != 0 {
		t.Fatalf("empty kernel result: %v %v", empty, err)
	}
	if _, err := windowF32Stats([]float64{math.Inf(1)}, 11.5); err == nil {
		t.Fatal("invalid f32 value accepted")
	}
}
