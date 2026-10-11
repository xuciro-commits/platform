package flow

import (
	"context"
	_ "embed"
	"fmt"
	"math"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// This is the supplied Workshop telemetry kernel, with its original packed
// f32 ABI. It is fixed trusted platform code, not an uploaded tenant module.
// Regenerate the binary from kernel/telemetry_stats.wat using wat2wasm.
//
//go:embed kernel/telemetry_stats.wasm
var telemetryStatsKernel []byte

// windowF32Stats runs only on the outside preparation lane. Each worker owns
// its bounded memory; cancellation/timeout closes the module. Errors refuse
// the whole prepared batch rather than switching to a different numeric rule.
func windowF32Stats(values []float64, threshold float64) (map[string]float64, error) {
	if len(values) > 1_000_000 || math.IsNaN(threshold) || math.IsInf(threshold, 0) {
		return nil, fmt.Errorf("invalid telemetry kernel input bound")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(128).WithCloseOnContextDone(true))
	defer runtime.Close(ctx)
	module, err := runtime.Instantiate(ctx, telemetryStatsKernel)
	if err != nil {
		return nil, fmt.Errorf("telemetry kernel: %w", err)
	}
	memory := module.Memory()
	inputBytes := uint32(len(values)) * 4
	output := ((inputBytes+65535)/65536)*65536 + 64
	pages := (output + 20 + 65535) / 65536
	if current := memory.Size() / 65536; pages > current {
		if _, ok := memory.Grow(pages - current); !ok {
			return nil, fmt.Errorf("telemetry kernel memory budget exceeded")
		}
	}
	for i, value := range values {
		v := float32(value)
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || !memory.WriteUint32Le(uint32(i)*4, math.Float32bits(v)) {
			return nil, fmt.Errorf("telemetry reading cannot be represented as finite f32")
		}
	}
	if _, err := module.ExportedFunction("stats").Call(ctx, 0, uint64(len(values)), api.EncodeF32(float32(threshold)), uint64(output)); err != nil {
		return nil, fmt.Errorf("telemetry kernel execution: %w", err)
	}
	result := map[string]float64{"count": float64(len(values))}
	for i, name := range []string{"sum", "mean", "min", "max", "above"} {
		value, ok := memory.ReadFloat32Le(output + uint32(i)*4)
		if !ok || math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, fmt.Errorf("telemetry kernel produced an invalid %s", name)
		}
		result[name] = float64(value)
	}
	return result, nil
}
