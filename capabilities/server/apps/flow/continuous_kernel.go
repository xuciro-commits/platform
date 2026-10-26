package flow

import (
	"context"
	"fmt"
	"math"
	"time"
)

// windowF32Stats preserves the fixed kernel's packed f32 input/output contract:
// round readings and threshold to f32, sum in order in f64, then round outputs.
// It runs on the outside preparation lane and shares its worker cancellation.
func windowF32Stats(values []float64, threshold float64, cancelled func() bool) (map[string]float64, error) {
	if len(values) > 1_000_000 || math.IsNaN(threshold) || math.IsInf(threshold, 0) {
		return nil, fmt.Errorf("invalid telemetry kernel input bound")
	}
	deadline := time.Now().Add(2 * time.Second)
	check := func() error {
		if cancelled() {
			return fmt.Errorf("telemetry kernel execution: %w", context.Canceled)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("telemetry kernel execution: %w", context.DeadlineExceeded)
		}
		return nil
	}
	cutoff := float32(threshold)
	sum, mean, above := 0.0, 0.0, 0
	lo, hi := float32(math.MaxFloat32), -float32(math.MaxFloat32)
	for i, value := range values {
		if i%1024 == 0 {
			if err := check(); err != nil {
				return nil, err
			}
		}
		v := float32(value)
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, fmt.Errorf("telemetry reading cannot be represented as finite f32")
		}
		sum += float64(v)
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
		if v > cutoff {
			above++
		}
	}
	if err := check(); err != nil {
		return nil, err
	}
	if len(values) == 0 {
		lo, hi = 0, 0
	} else {
		mean = sum / float64(len(values))
	}
	result := map[string]float64{"count": float64(len(values))}
	outputs := [...]float32{float32(sum), float32(mean), lo, hi, float32(above)}
	for i, name := range [...]string{"sum", "mean", "min", "max", "above"} {
		value := float64(outputs[i])
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("telemetry kernel produced an invalid %s", name)
		}
		result[name] = value
	}
	return result, nil
}
