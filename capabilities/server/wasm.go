package platformserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
	"platformserver/platform"
)

type WasmRequest struct {
	ABI    string                   `json:"abi,omitempty"`
	Data   *WasmDataInput           `json:"data,omitempty"`
	Module []byte                   `json:"module"`
	Digest string                   `json:"digest"`
	Input  json.RawMessage          `json:"input,omitempty"`
	Limits platform.OperationLimits `json:"limits"`
}

// WasmDataInput is one host-projected, frozen call. No paths, URLs or store capabilities enter the guest.
type WasmDataInput struct {
	Tenant string `json:"tenant"`
	Call   string `json:"call"`
	Digest string `json:"digest"`
	Bytes  []byte `json:"bytes"`
}
type WasmResponse struct {
	Output            json.RawMessage `json:"output,omitempty"`
	Error             string          `json:"error,omitempty"`
	InstantiateMicros int64           `json:"instantiateMicros"`
	ExecuteMicros     int64           `json:"executeMicros"`
}
type WasmWorker interface {
	Execute(context.Context, WasmRequest) (WasmResponse, error)
}

// WasmEngine is used only by the separate worker program. A shared compilation
// cache contains immutable code, while every call owns its runtime/stdio/memory.
type WasmEngine struct {
	cache   wazero.CompilationCache
	slots   chan struct{}
	mu      sync.Mutex
	bytes   int
	modules map[string]bool
}

func NewWasmEngine(concurrency int) *WasmEngine {
	if concurrency < 1 || concurrency > 32 {
		concurrency = 4
	}
	return &WasmEngine{cache: wazero.NewCompilationCache(), slots: make(chan struct{}, concurrency), modules: map[string]bool{}}
}
func (e *WasmEngine) Close(ctx context.Context) error { return e.cache.Close(ctx) }
func (e *WasmEngine) Execute(ctx context.Context, q WasmRequest) (WasmResponse, error) {
	var result WasmResponse
	hash := sha256.Sum256(q.Module)
	if len(q.Module) == 0 || len(q.Module) > 16<<20 || hex.EncodeToString(hash[:]) != q.Digest || q.Limits.MemoryPages < 1 || q.Limits.MemoryPages > 4096 || q.Limits.TimeoutMillis < 1 || q.Limits.TimeoutMillis > 30000 || q.Limits.MaxOutputBytes < 1 || q.Limits.MaxOutputBytes > 48<<10 || q.Limits.MaxInputBytes < 1 || q.Limits.MaxInputBytes > 1<<20 || len(q.Input) > q.Limits.MaxInputBytes || q.Limits.StagedOutputBytes < 0 || q.Limits.StagedOutputBytes > 16<<20 {
		return result, fmt.Errorf("invalid worker module or resource request")
	}
	abi := q.ABI
	if abi == "" {
		abi = platform.WasmCommandABI
	}
	if abi != platform.WasmCommandABI && abi != platform.WasmDataABI || abi == platform.WasmDataABI && q.Limits.StagedOutputBytes <= q.Limits.MaxOutputBytes {
		return result, fmt.Errorf("unknown worker ABI")
	}
	if q.Data != nil {
		sum := sha256.Sum256(q.Data.Bytes)
		if abi != platform.WasmDataABI || len(q.Input) != 0 || q.Data.Tenant == "" || !strings.HasPrefix(q.Data.Call, q.Data.Tenant+":") || len(q.Data.Call) <= len(q.Data.Tenant)+1 || q.Limits.DataInputBytes <= q.Limits.MaxInputBytes || q.Limits.DataInputBytes > 64<<20 || len(q.Data.Bytes) == 0 || len(q.Data.Bytes) > q.Limits.DataInputBytes || hex.EncodeToString(sum[:]) != q.Data.Digest || !json.Valid(q.Data.Bytes) {
			return result, fmt.Errorf("invalid call-owned input channel")
		}
	} else if q.Limits.DataInputBytes != 0 {
		return result, fmt.Errorf("declared input channel is missing")
	}
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(q.Limits.TimeoutMillis)*time.Millisecond)
	defer cancel()
	e.mu.Lock()
	cache := e.cache
	if !e.modules[q.Digest] {
		if len(e.modules) >= 64 || e.bytes+len(q.Module) > 64<<20 {
			// Cache admission is an optimisation, never an execution gate.
			// Oversized working sets use a call-owned disposable cache.
			cache = wazero.NewCompilationCache()
			defer cache.Close(context.Background())
		} else {
			e.modules[q.Digest] = true
			e.bytes += len(q.Module)
		}
	}
	e.mu.Unlock()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler().WithCompilationCache(cache).WithMemoryLimitPages(q.Limits.MemoryPages).WithCloseOnContextDone(true))
	defer r.Close(context.Background())
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		return result, err
	}
	compiled, err := r.CompileModule(ctx, q.Module)
	if err != nil {
		return result, err
	}
	if err := checkCompiledWasm(compiled, abi); err != nil {
		return result, err
	}
	input := q.Input
	channel := &boundedBuffer{limit: q.Limits.StagedOutputBytes}
	if abi == platform.WasmDataABI {
		if q.Data != nil {
			input = q.Data.Bytes
		}
		_, err = r.NewHostModuleBuilder("platform_data_v2").
			NewFunctionBuilder().WithFunc(func() uint32 { return uint32(len(input)) }).Export("input_size").
			NewFunctionBuilder().WithFunc(func(_ context.Context, m api.Module, offset, ptr, size uint32) uint32 {
			if uint64(offset)+uint64(size) > uint64(len(input)) {
				return 1
			}
			if m.Memory() == nil || !m.Memory().Write(ptr, input[offset:offset+size]) {
				return 1
			}
			return 0
		}).Export("read_input").
			NewFunctionBuilder().WithFunc(func(_ context.Context, m api.Module, ptr, size uint32) uint32 {
			if m.Memory() == nil {
				return 1
			}
			raw, ok := m.Memory().Read(ptr, size)
			if !ok {
				return 1
			}
			if _, err := channel.Write(raw); err != nil {
				return 1
			}
			return 0
		}).Export("write_output").Instantiate(ctx)
		if err != nil {
			return result, err
		}
	}
	stdout := &boundedBuffer{limit: q.Limits.MaxOutputBytes + 1024}
	stderr := &boundedBuffer{limit: 4096}
	stdin := q.Input
	if abi == platform.WasmDataABI {
		stdin = []byte(`{"abi":"platform-wasip1-data/v2"}`)
	}
	config := wazero.NewModuleConfig().WithName("").WithStartFunctions().WithStdin(bytes.NewReader(stdin)).WithStdout(stdout).WithStderr(stderr)
	started := time.Now()
	mod, err := r.InstantiateModule(ctx, compiled, config)
	result.InstantiateMicros = time.Since(started).Microseconds()
	if err != nil {
		return result, fmt.Errorf("Wasm instantiation: %w", err)
	}
	started = time.Now()
	_, err = mod.ExportedFunction("_start").Call(ctx)
	result.ExecuteMicros = time.Since(started).Microseconds()
	var exit *sys.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 0) {
		return result, fmt.Errorf("Wasm command failed: %w", err)
	}
	if stdout.exceeded {
		return result, fmt.Errorf("Wasm output exceeds its byte budget")
	}
	v, err := platform.DecodeValue(stdout.buf.Bytes(), stdout.limit)
	if err != nil {
		return result, err
	}
	envelope, ok := v.(map[string]any)
	if !ok {
		return result, fmt.Errorf("Wasm command must return an ABI envelope")
	}
	success, ok := envelope["ok"].(bool)
	if !ok {
		return result, fmt.Errorf("Wasm command envelope needs ok")
	}
	if success && abi == platform.WasmDataABI && envelope["channel"] == true {
		if len(envelope) != 2 || channel.exceeded || channel.buf.Len() == 0 || !json.Valid(channel.buf.Bytes()) {
			return result, fmt.Errorf("invalid or over-budget call output channel")
		}
		result.Output = bytes.Clone(channel.buf.Bytes())
		return result, nil
	}
	if channel.buf.Len() > 0 || channel.exceeded {
		return result, fmt.Errorf("unacknowledged call output channel")
	}
	if success {
		if len(envelope) != 2 {
			return result, fmt.Errorf("Wasm success envelope has unexpected fields")
		}
		value, exists := envelope["value"]
		if !exists {
			return result, fmt.Errorf("Wasm success envelope omits value")
		}
		result.Output, err = json.Marshal(value)
		if err != nil || len(result.Output) > q.Limits.MaxOutputBytes {
			return result, fmt.Errorf("Wasm value exceeds its byte budget")
		}
		return result, nil
	}
	problem, ok := envelope["error"].(map[string]any)
	code, codeOK := problem["code"].(string)
	message, messageOK := problem["message"].(string)
	if len(envelope) != 2 || !ok || len(problem) != 2 || !codeOK || !messageOK || code == "" || len(message) > 4096 {
		return result, fmt.Errorf("Wasm error envelope is invalid")
	}
	return result, fmt.Errorf("%s: %s", code, message)
}
func checkCompiledWasm(m wazero.CompiledModule, abi string) error {
	start, ok := m.ExportedFunctions()["_start"]
	if !ok || len(start.ParamTypes()) != 0 || len(start.ResultTypes()) != 0 {
		return fmt.Errorf("module must export the WASIp1 command _start")
	}
	for _, f := range m.ImportedFunctions() {
		module, name, _ := f.Import()
		if module == "wasi_snapshot_preview1" {
			continue
		}
		parameters := -1
		if abi == platform.WasmDataABI && module == "platform_data_v2" {
			switch name {
			case "input_size":
				parameters = 0
			case "read_input":
				parameters = 3
			case "write_output":
				parameters = 2
			}
		}
		if len(f.ParamTypes()) != parameters || len(f.ResultTypes()) != 1 || f.ResultTypes()[0] != api.ValueTypeI32 {
			return fmt.Errorf("module requests undeclared host imports")
		}
		for _, parameter := range f.ParamTypes() {
			if parameter != api.ValueTypeI32 {
				return fmt.Errorf("data channel parameters must be i32")
			}
		}
	}
	if len(m.ImportedMemories()) > 0 {
		return fmt.Errorf("module cannot import shared memory")
	}
	return nil
}
func ValidateWasm(ctx context.Context, module []byte, bindingABI ...string) error {
	abi := platform.WasmCommandABI
	if len(bindingABI) > 0 && bindingABI[0] != "" {
		abi = bindingABI[0]
	}
	if abi != platform.WasmCommandABI && abi != platform.WasmDataABI {
		return fmt.Errorf("unknown worker ABI")
	}
	if len(module) == 0 || len(module) > 16<<20 {
		return fmt.Errorf("module exceeds its byte bound")
	}
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter().WithMemoryLimitPages(4096).WithCloseOnContextDone(true))
	defer r.Close(context.Background())
	m, err := r.CompileModule(ctx, module)
	if err != nil {
		return err
	}
	defer m.Close(ctx)
	return checkCompiledWasm(m, abi)
}

type boundedBuffer struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buf.Len() {
		b.exceeded = true
		remaining := b.limit - b.buf.Len()
		if remaining > 0 {
			b.buf.Write(p[:remaining])
		}
		return len(p), fmt.Errorf("output exceeds byte budget")
	}
	return b.buf.Write(p)
}

// The worker listens only on an owner-only Unix socket, never a tenant HTTP
// route. Its process receives no database/store credentials or application API.
func ServeWasmWorker(ctx context.Context, socket string, concurrency int) error {
	if socket == "" {
		return fmt.Errorf("worker requires a Unix socket")
	}
	listener, err := listenComputeSocket(socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(socket)
	if err := os.Chmod(socket, 0660); err != nil {
		return err
	}
	engine := NewWasmEngine(concurrency)
	defer engine.Close(context.Background())
	mux := http.NewServeMux()
	mux.HandleFunc("POST /execute", func(w http.ResponseWriter, r *http.Request) {
		var q WasmRequest
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 112<<20))
		d.DisallowUnknownFields()
		if d.Decode(&q) != nil || d.Decode(new(any)) != io.EOF {
			http.Error(w, "invalid worker request", http.StatusBadRequest)
			return
		}
		result, err := engine.Execute(r.Context(), q)
		if err != nil {
			result.Error = err.Error()
		}
		w.Header().Set("Content-Type", "application/json")
		encoder := json.NewEncoder(w)
		encoder.SetEscapeHTML(false)
		encoder.Encode(result)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	server := &http.Server{Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 35 * time.Second, MaxHeaderBytes: 4096}
	stopped := make(chan struct{})
	go func() {
		<-ctx.Done()
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(stop)
		close(stopped)
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		<-stopped
		return nil
	}
	return err
}

type socketWasmWorker struct{ socket string }

func EnvironmentWasmWorker() WasmWorker {
	return socketWasmWorker{socket: os.Getenv("PLATFORM_WASM_WORKER_SOCKET")}
}
func (s socketWasmWorker) Execute(ctx context.Context, q WasmRequest) (WasmResponse, error) {
	var result WasmResponse
	if s.socket == "" {
		return result, fmt.Errorf("The isolated Go/Wasm worker is not configured")
	}
	body, err := json.Marshal(q)
	if err != nil {
		return result, err
	}
	client := http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", s.socket)
	}}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://worker/execute", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return result, fmt.Errorf("worker refused request")
	}
	budget := 64 << 10
	if q.ABI == platform.WasmDataABI && q.Limits.StagedOutputBytes > 0 {
		budget = q.Limits.StagedOutputBytes + (16 << 10)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(budget)+1))
	if err != nil || len(raw) > budget {
		return result, fmt.Errorf("worker reply exceeds its declared channel")
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	return result, nil
}
