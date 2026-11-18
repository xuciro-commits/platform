package protocol

import (
	"context"
	"encoding/json"
	"platformserver/kernel/values"
	"platformserver/platform"
)

type CapabilityInvocation struct {
	Ref              platform.AssetRef           `json:"ref"`
	Key              string                      `json:"key"`
	Inputs           json.RawMessage             `json:"inputs"`
	Target           string                      `json:"target,omitempty"`
	Version          int                         `json:"version,omitempty"`
	ExpectedRevision *uint32                     `json:"expectedRevision,omitempty"`
	Sources          []string                    `json:"sources,omitempty"`
	Record           string                      `json:"record,omitempty"`
	Bindings         map[string]platform.Binding `json:"bindings,omitempty"`
}

type CapabilityResult struct {
	Issues []platform.FieldIssue `json:"issues,omitempty"`
	Ref    platform.AssetRef     `json:"ref"`
	State  string                `json:"state"`
	Call   string                `json:"call,omitempty"`
	Result json.RawMessage       `json:"result,omitempty"`
}

func ValidateRecordJSON(raw []byte) error {
	return values.ValidateRecord(raw, 2*platform.ExprMaxDepth+16)
}

type OperationExecutor interface {
	Compute(context.Context, string, json.RawMessage) (json.RawMessage, error)
}

type CodeBuildResult struct {
	Module      string `json:"module,omitempty"`
	SourceHash  string `json:"sourceHash"`
	BuildHash   string `json:"buildHash"`
	Toolchain   string `json:"toolchain"`
	Diagnostics string `json:"diagnostics,omitempty"`
}

type WasmRequest struct {
	ABI    string                   `json:"abi,omitempty"`
	Data   *WasmDataInput           `json:"data,omitempty"`
	Module []byte                   `json:"module"`
	Digest string                   `json:"digest"`
	Input  json.RawMessage          `json:"input,omitempty"`
	Limits platform.OperationLimits `json:"limits"`
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
	Compile(context.Context, platform.CodeBuildRequest) (CodeBuildResult, []byte, error)
}

type ComputeSDKRequest struct {
	ABI    string               `json:"abi,omitempty"`
	Input  platform.ValueSchema `json:"input"`
	Output platform.ValueSchema `json:"output"`
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
