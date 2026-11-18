package platform

import (
	"context"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/values"
	"slices"
	"strconv"
	"strings"
	"time"
)

type CapabilityInvocation struct {
	Ref              AssetRef           `json:"ref"`
	Key              string             `json:"key"`
	Inputs           json.RawMessage    `json:"inputs"`
	Target           string             `json:"target,omitempty"`
	Version          int                `json:"version,omitempty"`
	ExpectedRevision *uint32            `json:"expectedRevision,omitempty"`
	Sources          []string           `json:"sources,omitempty"`
	Record           string             `json:"record,omitempty"`
	Bindings         map[string]Binding `json:"bindings,omitempty"`
}

type CapabilityResult struct {
	Issues []FieldIssue    `json:"issues,omitempty"`
	Ref    AssetRef        `json:"ref"`
	State  string          `json:"state"`
	Call   string          `json:"call,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

const WasmCommandABI = "platform-wasip1-json/v1"
const WasmDataABI = "platform-wasip1-data/v2"

type ValueSchema = values.ValueSchema

func DecodeValue(raw []byte, limit int) (any, error) { return values.Decode(raw, limit) }

func ValidateRecordJSON(raw []byte) error {
	return values.ValidateRecord(raw, 2*ExprMaxDepth+16)
}

type OperationBinding struct {
	Kind   string `json:"kind"`
	Module string `json:"module,omitempty"`
	ABI    string `json:"abi,omitempty"`
}
type OperationLimits struct {
	TimeoutMillis     int    `json:"timeoutMillis"`
	MemoryPages       uint32 `json:"memoryPages"`
	MaxInputBytes     int    `json:"maxInputBytes"`
	MaxOutputBytes    int    `json:"maxOutputBytes"`
	StagedOutputBytes int    `json:"stagedOutputBytes,omitempty"`
	DataInputBytes    int    `json:"dataInputBytes,omitempty"`
}
type Operation struct {
	Rules       *RuleTablePlan   `json:"rules,omitempty"`
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Input       ValueSchema      `json:"input"`
	Output      ValueSchema      `json:"output"`
	Roles       []string         `json:"roles"`
	Binding     OperationBinding `json:"binding"`
	Limits      OperationLimits  `json:"limits"`
}

func (o Operation) Check() error {
	if !functionName.MatchString(o.Name) || strings.TrimSpace(o.Title) == "" || len(o.Title) > 256 || len(o.Description) > 1024 || len(o.Roles) == 0 {
		return fmt.Errorf("operation needs a name, title and callable roles")
	}
	if err := o.Input.Check(); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	if err := o.Output.Check(); err != nil {
		return fmt.Errorf("output: %w", err)
	}
	if o.Binding.Kind != "native" && o.Binding.Kind != "wasm" || o.Binding.Kind == "wasm" && (len(o.Binding.Module) != 64 || (o.Binding.ABI != WasmCommandABI && o.Binding.ABI != WasmDataABI)) || o.Binding.Kind == "native" && (o.Binding.Module != "" || o.Binding.ABI != "") {
		return fmt.Errorf("operation needs a native binding or pinned WASIp1 command")
	}
	if o.Binding.Kind == "wasm" {
		for _, c := range o.Binding.Module {
			if !strings.ContainsRune("0123456789abcdef", c) {
				return fmt.Errorf("invalid module digest")
			}
		}
	}
	if o.Limits.TimeoutMillis < 1 || o.Limits.TimeoutMillis > 30000 || o.Limits.MemoryPages < 1 || o.Limits.MemoryPages > 4096 || o.Limits.MaxInputBytes < 1 || o.Limits.MaxInputBytes > 1<<20 || o.Limits.MaxOutputBytes < 1 || o.Limits.MaxOutputBytes > 48<<10 {
		return fmt.Errorf("operation needs bounded time, memory and JSON bytes")
	}
	if o.Limits.StagedOutputBytes < 0 || o.Limits.StagedOutputBytes > 16<<20 ||
		o.Limits.StagedOutputBytes > 0 && o.Limits.StagedOutputBytes <= o.Limits.MaxOutputBytes {
		return fmt.Errorf("operation staged output must exceed the inline budget and stay within 16 MiB")
	}
	if o.Binding.ABI == WasmDataABI && o.Limits.StagedOutputBytes == 0 {
		return fmt.Errorf("the v2 data ABI needs a bounded call output channel")
	}
	if o.Limits.DataInputBytes < 0 || o.Limits.DataInputBytes > 64<<20 || o.Limits.DataInputBytes > 0 && (o.Binding.Kind != "wasm" || o.Binding.ABI != WasmDataABI || o.Limits.DataInputBytes <= o.Limits.MaxInputBytes) {
		return fmt.Errorf("operation data input requires the v2 read channel, above stdin and within 64 MiB")
	}
	seen := map[string]bool{}
	for _, r := range o.Roles {
		if r == "" || seen[r] {
			return fmt.Errorf("operation roles must be unique")
		}
		seen[r] = true
	}
	return nil
}

type OperationExecutor interface {
	Compute(context.Context, string, json.RawMessage) (json.RawMessage, error)
}
type OperationRequest struct {
	App      string          `json:"app,omitempty"`
	Name     string          `json:"name"`
	Version  int             `json:"version,omitempty"`
	Key      string          `json:"key"`
	Inputs   json.RawMessage `json:"inputs"`
	Sources  []string        `json:"sources,omitempty"`
	Target   string          `json:"target,omitempty"`
	OnBehalf string          `json:"onBehalf,omitempty"`
	Release  *string         `json:"release,omitempty"`
}
type OperationCall struct {
	Applications []AssetBinding `json:"applications,omitempty" field:"readonly" type:"json"`
	OwnerVersion string         `json:"ownerVersion,omitempty"`
	ID           string         `json:"id"`
	Definition   string         `json:"definition"`
	InputHash    string         `json:"inputHash"`
	Version      int            `json:"version,omitempty"`
	Module       string         `json:"module,omitempty"`
	Sources      []string       `json:"sources,omitempty"`
	Dependencies string         `json:"dependencies"`
	Release      string         `json:"release,omitempty"`
}
type OperationResult struct {
	OwnerVersion string          `json:"ownerVersion,omitempty"`
	Ref          *AssetRef       `json:"ref,omitempty"`
	Definition   string          `json:"definition,omitempty"`
	Version      int             `json:"version,omitempty"`
	Module       string          `json:"module,omitempty"`
	Dependencies string          `json:"dependencies,omitempty"`
	Release      string          `json:"release,omitempty"`
	ID           string          `json:"id"`
	State        string          `json:"state"`
	Output       json.RawMessage `json:"output,omitempty"`
	Error        string          `json:"error,omitempty"`
	Generation   uint32          `json:"generation"`
	Millis       int64           `json:"millis,omitempty"`
}

func (c Caller) RequestOperation(r *pb.ChangeRecord, q OperationRequest) (OperationCall, *Error) {
	if c.rt != nil {
		return c.rt.RequestOperation(c, r, q)
	}
	return OperationCall{}, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operations require an accepted decision")
}
func (c Caller) OperationResult(id string) (OperationResult, *Error) {
	if c.rt != nil {
		return c.rt.OperationResult(c, id)
	}
	return OperationResult{}, notFound()
}

func (c Caller) CancelOperation(r *pb.ChangeRecord, id string) *Error {
	if c.rt != nil {
		return c.rt.CancelOperation(c, r, id)
	}
	return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Cancellation requires an accepted decision")
}

type CodeBuildRequest struct {
	ABI      string      `json:"abi,omitempty"`
	Language string      `json:"language"`
	Source   string      `json:"source"`
	Input    ValueSchema `json:"input"`
	Output   ValueSchema `json:"output"`
}
type CodeBuildResult struct {
	Module      string `json:"module,omitempty"`
	SourceHash  string `json:"sourceHash"`
	BuildHash   string `json:"buildHash"`
	Toolchain   string `json:"toolchain"`
	Diagnostics string `json:"diagnostics,omitempty"`
}

func (c Caller) RequestCompilation(r *pb.ChangeRecord, key, target string, q CodeBuildRequest) (OperationCall, *Error) {
	if c.rt != nil {
		return c.rt.RequestCompilation(c, r, key, target, q)
	}
	return OperationCall{}, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Compilation requires an accepted decision")
}

type Job struct {
	Name  string        `json:"name"`
	Title string        `json:"title"`
	Every time.Duration `json:"every"`
}

type Retry struct {
	Initial, Max time.Duration
	Attempts     int
}

func (r Retry) After(attempts int) time.Duration {
	return min(r.Initial<<max(attempts-1, 0), r.Max)
}

type Setting struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Type        string   `json:"type" enum:"boolean,integer,text,choice"`
	Default     string   `json:"default"`
	Choices     []string `json:"choices,omitempty"`
}

func (s Setting) Accepts(v string) bool {
	switch s.Type {
	case "boolean":
		return v == "true" || v == "false"
	case "integer":
		_, err := strconv.Atoi(v)
		return err == nil
	case "choice":
		return slices.Contains(s.Choices, v)
	}
	return s.Type == "text"
}

func (c Caller) Setting(name string) string {
	if c.rt == nil {
		return ""
	}
	return c.rt.Setting(c, name)
}

type Recipient struct {
	Member                string
	Structure, Unit, Role string
	AppRole               string
}

type Notification struct {
	ID       string    `json:"id" source:"record"`
	Member   string    `json:"member" source:"record"`
	App      string    `json:"app" source:"record"`
	Title    string    `json:"title" source:"generated" from:"Ref"`
	Body     string    `json:"body,omitempty" source:"generated" from:"Ref"`
	Ref      string    `json:"ref,omitempty" source:"record"`
	Key      string    `json:"key,omitempty" source:"record"`
	At       time.Time `json:"at"`
	Read     bool      `json:"read"`
	Withheld bool      `json:"withheld,omitempty"`
}

func (c Caller) Notify(n Notification, now time.Time, to ...Recipient) []string {
	if c.rt == nil {
		return nil
	}
	return c.rt.Notify(c, n, now, to)
}

type WasmRequest struct {
	ABI    string          `json:"abi,omitempty"`
	Data   *WasmDataInput  `json:"data,omitempty"`
	Module []byte          `json:"module"`
	Digest string          `json:"digest"`
	Input  json.RawMessage `json:"input,omitempty"`
	Limits OperationLimits `json:"limits"`
}

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

type CodeCompiler interface {
	Compile(context.Context, CodeBuildRequest) (CodeBuildResult, []byte, error)
}

type ComputeSDKRequest struct {
	ABI    string      `json:"abi,omitempty"`
	Input  ValueSchema `json:"input"`
	Output ValueSchema `json:"output"`
}

type ComputeSDK struct {
	Source string `json:"source"`
}

type OperationInputArtifact struct {
	Tenant     string `json:"tenant"`
	Call       string `json:"call"`
	Member     string `json:"member"`
	Definition string `json:"definition"`
	InputHash  string `json:"inputHash"`
	Ticket     string `json:"ticket"`
	Digest     string `json:"digest"`
	Size       int    `json:"size"`
}
