package compute

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
	"go/format"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"platformserver/platform"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

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
func (e *WasmEngine) Execute(ctx context.Context, q platform.WasmRequest) (platform.WasmResponse, error) {
	var result platform.WasmResponse
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
		var q platform.WasmRequest
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

func EnvironmentWasmWorker() platform.WasmWorker {
	return socketWasmWorker{socket: os.Getenv("PLATFORM_WASM_WORKER_SOCKET")}
}
func (s socketWasmWorker) Execute(ctx context.Context, q platform.WasmRequest) (platform.WasmResponse, error) {
	var result platform.WasmResponse
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

type ContainerCompiler struct{ GoImage, TinyGoImage string }

func EnvironmentCompiler() platform.CodeCompiler {
	if socket := os.Getenv("PLATFORM_CODE_BUILDER_SOCKET"); socket != "" {
		return socketCodeCompiler{socket}
	}
	return ContainerCompiler{GoImage: os.Getenv("PLATFORM_GO_WASM_IMAGE"), TinyGoImage: os.Getenv("PLATFORM_TINYGO_WASM_IMAGE")}
}

type codeCompilerResponse struct {
	Result platform.CodeBuildResult `json:"result"`
	Module []byte                   `json:"module,omitempty"`
	Error  string                   `json:"error,omitempty"`
}
type socketCodeCompiler struct{ socket string }

func (s socketCodeCompiler) Compile(ctx context.Context, q platform.CodeBuildRequest) (platform.CodeBuildResult, []byte, error) {
	raw, _ := json.Marshal(q)
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", s.socket)
	}}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://builder/compile", bytes.NewReader(raw))
	if err != nil {
		return platform.CodeBuildResult{}, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return platform.CodeBuildResult{}, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return platform.CodeBuildResult{}, nil, fmt.Errorf("isolated compiler is busy or refused input")
	}
	var answer codeCompilerResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 24<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&answer); err != nil {
		return answer.Result, nil, err
	}
	if answer.Error != "" {
		return answer.Result, nil, errors.New(answer.Error)
	}
	return answer.Result, answer.Module, nil
}

// The build driver owns only container creation. It never executes Wasm or
// receives business/database credentials; compiler guests inherit no socket.
func ServeCodeBuilder(ctx context.Context, socket string, compiler ContainerCompiler) error {
	if socket == "" || !pinnedImage.MatchString(compiler.GoImage) || !pinnedImage.MatchString(compiler.TinyGoImage) {
		return fmt.Errorf("code builder requires a Unix socket and both pinned toolchains")
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
	slots := make(chan struct{}, 2)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("POST /compile", func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "compiler capacity", http.StatusTooManyRequests)
			return
		}
		var q platform.CodeBuildRequest
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10))
		d.DisallowUnknownFields()
		if d.Decode(&q) != nil {
			http.Error(w, "invalid build input", http.StatusBadRequest)
			return
		}
		buildCtx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
		defer cancel()
		result, module, err := compiler.Compile(buildCtx, q)
		answer := codeCompilerResponse{Result: result, Module: module}
		if err != nil {
			answer.Error = err.Error()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(answer)
	})
	server := &http.Server{Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 185 * time.Second, WriteTimeout: 185 * time.Second, MaxHeaderBytes: 4096}
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

var pinnedImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*@sha256:[0-9a-f]{64}$`)

func (c ContainerCompiler) Compile(ctx context.Context, q platform.CodeBuildRequest) (platform.CodeBuildResult, []byte, error) {
	var result platform.CodeBuildResult
	hash := sha256.Sum256([]byte(q.Source))
	result.SourceHash = hex.EncodeToString(hash[:])
	result.BuildHash, _ = platform.CanonicalDigest(q)
	image := c.GoImage
	if q.Language == "tinygo" {
		image = c.TinyGoImage
	}
	result.Toolchain = image
	if !pinnedImage.MatchString(image) {
		return result, nil, fmt.Errorf("%s compiler requires an owner-configured toolchain image pinned by sha256", q.Language)
	}
	if q.Language != "go" && q.Language != "tinygo" || len(q.Source) == 0 || len(q.Source) > 256<<10 {
		return result, nil, fmt.Errorf("source requires a bounded Go/TinyGo profile")
	}
	sdk, err := GenerateComputeSDK(q.Input, q.Output, q.ABI)
	if err != nil {
		return result, nil, err
	}
	source, err := format.Source([]byte(q.Source))
	if err != nil {
		return result, nil, fmt.Errorf("source: %w", err)
	}
	// Fixed commands and an offline stdlib-only package. Build scripts, module
	// downloads, mounts, credentials and the daemon socket never enter the guest.
	command := "go build -trimpath -ldflags='-s -w' -o /tmp/module.wasm . >&2 && cat /tmp/module.wasm"
	if q.Language == "tinygo" {
		command = "tinygo build -target=wasip1 -o /tmp/module.wasm . >&2 && cat /tmp/module.wasm"
	}
	command = "mkdir /tmp/src && tar -xf - -C /tmp/src && cd /tmp/src && " + command
	args := []string{"create", "--interactive", "--pull=never", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--pids-limit=128", "--memory=1g", "--memory-swap=1g", "--cpus=2", "--user=65534:65534", "--tmpfs=/tmp:rw,nosuid,nodev,size=768m,mode=1777", "--workdir=/tmp", "--env=GOPROXY=off", "--env=GOSUMDB=off", "--env=GO111MODULE=off", "--env=GOWORK=off", "--env=GOENV=off", "--env=CGO_ENABLED=0", "--env=GOOS=wasip1", "--env=GOARCH=wasm", "--env=GOCACHE=/tmp/cache", "--env=XDG_CACHE_HOME=/tmp/cache", "--env=GOPATH=/tmp/gopath", image, "sh", "-c", command}
	created, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return result, nil, fmt.Errorf("create isolated compiler: %w", err)
	}
	id := strings.TrimSpace(string(created))
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		exec.CommandContext(cleanup, "docker", "rm", "-f", id).Run()
	}()
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, f := range []struct {
		name string
		data []byte
	}{{"function.go", source}, {"sdk.go", sdk}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0444, Size: int64(len(f.data))}); err != nil {
			return result, nil, err
		}
		tw.Write(f.data)
	}
	tw.Close()
	module := &boundedBuffer{limit: 16 << 20}
	diagnostics := &boundedBuffer{limit: 16 << 10}
	run := exec.CommandContext(ctx, "docker", "start", "--attach", "--interactive", id)
	run.Stdin = &archive
	run.Stdout = module
	run.Stderr = diagnostics
	err = run.Run()
	result.Diagnostics = diagnostics.buf.String()
	if err != nil || module.exceeded {
		return result, nil, fmt.Errorf("isolated %s compilation failed: %s", q.Language, result.Diagnostics)
	}
	if len(module.buf.Bytes()) < 8 || string(module.buf.Bytes()[:4]) != "\x00asm" {
		return result, nil, fmt.Errorf("compiler did not produce a Wasm module")
	}
	return result, module.buf.Bytes(), nil
}

// GenerateComputeSDK emits explicit typed decoding/encoding. TinyGo uses no
// reflection-based encoding/json path. The owner's schema remains authoritative.
func GenerateComputeSDK(input, output platform.ValueSchema, bindingABI ...string) ([]byte, error) {
	abi := platform.WasmCommandABI
	if len(bindingABI) > 0 && bindingABI[0] != "" {
		abi = bindingABI[0]
	}
	if abi != platform.WasmCommandABI && abi != platform.WasmDataABI {
		return nil, fmt.Errorf("unknown compute ABI")
	}
	if err := input.Check(); err != nil {
		return nil, err
	}
	if err := output.Check(); err != nil {
		return nil, err
	}
	g := sdkGenerator{}
	g.types = &strings.Builder{}
	g.code = &strings.Builder{}
	g.schema("Input", input)
	g.schema("Output", output)
	source := "package main\nimport(\"bytes\";\"errors\";\"io\";\"os\";\"strconv\";\"unicode/utf8\")\n" + g.types.String() + sdkHelpers + g.code.String() + `
func main(){raw,err:=io.ReadAll(io.LimitReader(os.Stdin,1048577));if err==nil&&len(raw)>1048576{err=errors.New("input limit")};var input Input;if err==nil{input,err=decodeInput(raw)};var output Output;if err==nil{output,err=Run(input)};if err!=nil{os.Stdout.Write(append(append([]byte("{\"ok\":false,\"error\":{\"code\":\"compute\",\"message\":"),jsonQuote(err.Error())...),[]byte("}}")...));return};encoded:=encodeOutput(output);os.Stdout.Write(append(append([]byte("{\"ok\":true,\"value\":"),encoded...),'}'))}
`
	if abi == platform.WasmDataABI {
		source = strings.Replace(source, `"unicode/utf8")`, `"unicode/utf8";"unsafe")`, 1)
		source = strings.Replace(source, `"io";`, "", 1)
		source = strings.Replace(source, `io.ReadAll(io.LimitReader(os.Stdin,1048577))`, `readDataInput()`, 1)
		source = strings.Replace(source, `len(raw)>1048576`, `len(raw)>67108864`, 1)
		source = strings.Replace(source, `os.Stdout.Write(append(append([]byte("{\"ok\":true,\"value\":"),encoded...),'}'))`, `if err=writeDataOutput(encoded);err!=nil{os.Stdout.Write([]byte("{\"ok\":false,\"error\":{\"code\":\"channel\",\"message\":\"output channel refused\"}}"));return};os.Stdout.Write([]byte("{\"ok\":true,\"channel\":true}"))`, 1)
		source += sdkDataHelpers
	}
	return format.Source([]byte(source))
}

const sdkDataHelpers = `
//go:wasmimport platform_data_v2 input_size
func dataInputSize() uint32
//go:wasmimport platform_data_v2 read_input
func dataReadInput(offset,ptr,size uint32) uint32
//go:wasmimport platform_data_v2 write_output
func dataWriteOutput(ptr,size uint32) uint32
func readDataInput()([]byte,error){size:=dataInputSize();if size==0||size>67108864{return nil,errors.New("input channel limit")};raw:=make([]byte,size);for offset:=uint32(0);offset<size;{n:=size-offset;if n>65536{n=65536};if dataReadInput(offset,uint32(uintptr(unsafe.Pointer(&raw[offset]))),n)!=0{return nil,errors.New("input channel refused")};offset+=n};return raw,nil}
func writeDataOutput(raw []byte)error{for offset:=0;offset<len(raw);{n:=len(raw)-offset;if n>65536{n=65536};if dataWriteOutput(uint32(uintptr(unsafe.Pointer(&raw[offset]))),uint32(n))!=0{return errors.New("output channel refused")};offset+=n};return nil}
`

type sdkGenerator struct{ types, code *strings.Builder }

func (g sdkGenerator) schema(name string, s platform.ValueSchema) string {
	typeName := name
	if s.Nullable {
		s.Nullable = false
		child := g.schema(name+"Value", s)
		fmt.Fprintf(g.types, "type %s = *%s\n", name, child)
		fmt.Fprintf(g.code, "func decode%s(raw []byte)(%s,error){if bytes.Equal(bytes.TrimSpace(raw),[]byte(\"null\")){return nil,nil};v,err:=decode%s(raw);if err!=nil{return nil,err};return &v,nil}\nfunc encode%s(v %s)[]byte{if v==nil{return []byte(\"null\")};return encode%s(*v)}\n", name, name, name+"Value", name, name, name+"Value")
		return name
	}
	switch s.Type {
	case "variant":
		keys := make([]string, 0, len(s.Variants))
		for tag := range s.Variants {
			keys = append(keys, tag)
		}
		slices.Sort(keys)
		fields := map[string]string{}
		seen := map[string]bool{}
		for i, tag := range keys {
			field := "As" + exportName(tag)
			if seen[field] {
				field += strconv.Itoa(i)
			}
			seen[field] = true
			fields[tag] = field
			g.schema(name+field, s.Variants[tag])
		}
		fmt.Fprintf(g.types, "type %s struct{Variant string;", name)
		for _, tag := range keys {
			fmt.Fprintf(g.types, "%s *%s%s;", fields[tag], name, fields[tag])
		}
		g.types.WriteString("}\n")
		fmt.Fprintf(g.code, "func decode%s(raw []byte)(out %s,err error){fields,err:=jsonFields(raw);if err!=nil{return out,err};tag,err:=jsonString(fields[%q]);if err!=nil{return out,err};out.Variant=tag;switch tag{", name, name, s.Discriminator)
		for _, tag := range keys {
			fmt.Fprintf(g.code, "case %q:v,err:=decode%s%s(raw);if err!=nil{return out,err};out.%s=&v;return out,nil;", tag, name, fields[tag], fields[tag])
		}
		g.code.WriteString("};return out,errors.New(\"unknown variant\")}\n")
		fmt.Fprintf(g.code, "func encode%s(v %s)[]byte{switch v.Variant{", name, name)
		for _, tag := range keys {
			fmt.Fprintf(g.code, "case %q:if v.%s!=nil{copy:=*v.%s;copy.%s=%q;return encode%s%s(copy)};", tag, fields[tag], fields[tag], exportName(s.Discriminator), tag, name, fields[tag])
		}
		g.code.WriteString("};return []byte(\"!invalid variant!\")}\n")
	case "object":
		keys := make([]string, 0, len(s.Properties))
		for key := range s.Properties {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		childTypes := map[string]string{}
		for _, key := range keys {
			childTypes[key] = g.schema(name+exportName(key), s.Properties[key])
		}
		fmt.Fprintf(g.types, "type %s struct{", name)
		for _, key := range keys {
			fmt.Fprintf(g.types, "%s %s;", exportName(key), childTypes[key])
		}
		g.types.WriteString("}\n")
		fmt.Fprintf(g.code, "func decode%s(raw []byte)(out %s,err error){fields,err:=jsonFields(raw);if err!=nil{return out,err};", name, name)
		for _, key := range keys {
			fmt.Fprintf(g.code, "if raw,ok:=fields[%q];ok{out.%s,err=decode%s(raw);if err!=nil{return out,err}};", key, exportName(key), name+exportName(key))
		}
		g.code.WriteString("return out,nil}\n")
		fmt.Fprintf(g.code, "func encode%s(v %s)[]byte{out:=[]byte{'{'};", name, name)
		for i, key := range keys {
			if i > 0 {
				g.code.WriteString("out=append(out,',');")
			}
			fmt.Fprintf(g.code, "out=append(out,%q...);out=append(out,encode%s(v.%s)...);", strconv.Quote(key)+":", name+exportName(key), exportName(key))
		}
		g.code.WriteString("return append(out,'}')}\n")
	case "array":
		child := g.schema(name+"Item", *s.Items)
		fmt.Fprintf(g.types, "type %s []%s\n", name, child)
		fmt.Fprintf(g.code, "func decode%s(raw []byte)(out %s,err error){items,err:=jsonItems(raw);if err!=nil{return out,err};for _,raw:=range items{v,err:=decode%s(raw);if err!=nil{return out,err};out=append(out,v)};return out,nil}\nfunc encode%s(v %s)[]byte{out:=[]byte{'['};for i,item:=range v{if i>0{out=append(out,',')};out=append(out,encode%s(item)...)};return append(out,']')}\n", name, name, name+"Item", name, name, name+"Item")
	default:
		base, parse, encode := "string", "jsonString(raw)", "jsonQuote(string(v))"
		switch s.Type {
		case "integer":
			base, parse, encode = "int64", "strconv.ParseInt(string(raw),10,64)", "strconv.AppendInt(nil,int64(v),10)"
		case "number":
			base, parse, encode = "float64", "strconv.ParseFloat(string(raw),64)", "strconv.AppendFloat(nil,float64(v),'g',-1,64)"
		case "boolean":
			base, parse, encode = "bool", "strconv.ParseBool(string(raw))", "strconv.AppendBool(nil,bool(v))"
		}
		fmt.Fprintf(g.types, "type %s = %s\n", name, base)
		fmt.Fprintf(g.code, "func decode%s(raw []byte)(%s,error){v,err:=%s;return %s(v),err}\nfunc encode%s(v %s)[]byte{return %s}\n", name, name, parse, name, name, name, encode)
	}
	return typeName
}
func exportName(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

const sdkHelpers = `
func jsonQuote(s string)[]byte{out:=[]byte{'"'};const hex="0123456789abcdef";for i:=0;i<len(s);i++{c:=s[i];if c=='"'||c=='\\'{out=append(out,'\\',c)}else if c<32{out=append(out,'\\','u','0','0',hex[c>>4],hex[c&15])}else{out=append(out,c)}};return append(out,'"')}
func jsonString(raw []byte)(string,error){raw=bytes.TrimSpace(raw);if len(raw)<2||raw[0]!='"'||raw[len(raw)-1]!='"'{return "",errors.New("string expected")};raw=raw[1:len(raw)-1];out:=[]byte{};for i:=0;i<len(raw);i++{c:=raw[i];if c!='\\'{out=append(out,c);continue};i++;if i>=len(raw){return "",errors.New("string escape incomplete")};switch raw[i]{case '"','\\','/':out=append(out,raw[i]);case 'b':out=append(out,8);case 'f':out=append(out,12);case 'n':out=append(out,10);case 'r':out=append(out,13);case 't':out=append(out,9);case 'u':if i+4>=len(raw){return "",errors.New("unicode escape incomplete")};n,err:=strconv.ParseUint(string(raw[i+1:i+5]),16,16);if err!=nil{return "",err};i+=4;r:=rune(n);if r>=0xd800&&r<=0xdbff{if i+6<len(raw)&&raw[i+1]=='\\'&&raw[i+2]=='u'{low,err:=strconv.ParseUint(string(raw[i+3:i+7]),16,16);if err==nil&&low>=0xdc00&&low<=0xdfff{r=0x10000+(r-0xd800)*1024+rune(low)-0xdc00;i+=6}else{r=utf8.RuneError}}else{r=utf8.RuneError}}else if r>=0xdc00&&r<=0xdfff{r=utf8.RuneError};out=utf8.AppendRune(out,r);default:return "",errors.New("invalid string escape")}};return string(out),nil}
func valueEnd(raw []byte) int {depth:=0;quoted:=false;escaped:=false;for i,c:=range raw{if quoted{if escaped{escaped=false}else if c=='\\'{escaped=true}else if c=='"'{quoted=false};continue};if c=='"'{quoted=true;continue};if c=='{'||c=='['{depth++};if c=='}'||c==']'{if depth==0{return i};depth--};if c==','&&depth==0{return i}};return len(raw)}
func jsonFields(raw []byte)(map[string][]byte,error){raw=bytes.TrimSpace(raw);if len(raw)<2||raw[0]!='{'||raw[len(raw)-1]!='}'{return nil,errors.New("object expected")};raw=bytes.TrimSpace(raw[1:len(raw)-1]);out:=map[string][]byte{};for len(raw)>0{if raw[0]!='"'{return nil,errors.New("field expected")};end:=1;for end<len(raw){if raw[end]=='\\'{end+=2;continue};if raw[end]=='"'{break};end++};if end>=len(raw){return nil,errors.New("field incomplete")};key,err:=strconv.Unquote(string(raw[:end+1]));if err!=nil{return nil,err};raw=bytes.TrimSpace(raw[end+1:]);if len(raw)==0||raw[0]!=':'{return nil,errors.New("value expected")};raw=bytes.TrimSpace(raw[1:]);end=valueEnd(raw);out[key]=bytes.TrimSpace(raw[:end]);raw=bytes.TrimSpace(raw[end:]);if len(raw)>0{if raw[0]!=','{return nil,errors.New("separator expected")};raw=bytes.TrimSpace(raw[1:])}};return out,nil}
func jsonItems(raw []byte)([][]byte,error){raw=bytes.TrimSpace(raw);if len(raw)<2||raw[0]!='['||raw[len(raw)-1]!=']'{return nil,errors.New("array expected")};raw=bytes.TrimSpace(raw[1:len(raw)-1]);out:=[][]byte{};for len(raw)>0{end:=valueEnd(raw);out=append(out,bytes.TrimSpace(raw[:end]));raw=bytes.TrimSpace(raw[end:]);if len(raw)>0{if raw[0]!=','{return nil,errors.New("separator expected")};raw=bytes.TrimSpace(raw[1:])}};return out,nil}
`

// A crashed stateless worker leaves a Unix socket in the shared runtime
// directory. Remove only a verified dead socket, never a live peer or file.
func listenComputeSocket(path string) (net.Listener, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("compute socket path is occupied by a file")
		}
		conn, err := net.DialTimeout("unix", path, 250*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil, fmt.Errorf("compute socket already has a live worker")
		}
		if !errors.Is(err, syscall.ECONNREFUSED) && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return net.Listen("unix", path)
}

func CheckComputeSocket(socket string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	defer client.CloseIdleConnections()
	request, _ := http.NewRequestWithContext(ctx, "GET", "http://compute/healthz", nil)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("compute worker is not healthy")
	}
	return nil
}
