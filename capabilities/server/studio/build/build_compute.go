package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"slices"
	"strconv"
	"strings"
	"time"
)

const SchemaCodeCompile = CodeType + ".compile"
const SchemaCodePublish = CodeType + ".publish"
const SchemaCodeCompiled = CodeType + ".compiled"

// Code is the single owner of source, build evidence and compute publications.
// Compiling changes no installed capability. Candidate activation owns release.
type Code struct {
	platform.Record
	Name          string                   `json:"name" field:"required,search" source:"record"`
	Title         string                   `json:"title" field:"required,search" source:"record"`
	Description   string                   `json:"description" type:"longtext" source:"record"`
	ABI           string                   `json:"abi,omitempty" field:"aside" title:"Compute ABI" source:"record"`
	BuiltABI      string                   `json:"builtABI,omitempty" field:"readonly,aside" title:"Built ABI" source:"record"`
	Language      string                   `json:"language" field:"required" choices:"go,tinygo"`
	Source        string                   `json:"source" field:"required" type:"longtext" source:"record"`
	Input         platform.ValueSchema     `json:"input" field:"aside" type:"json"`
	Output        platform.ValueSchema     `json:"output" field:"aside" type:"json"`
	Roles         []string                 `json:"roles" field:"aside"`
	Limits        platform.OperationLimits `json:"limits" field:"aside" type:"json"`
	State         string                   `json:"state" field:"readonly" choices:"draft,compiling,compiled,failed,published"`
	Call          string                   `json:"call,omitempty" field:"readonly" source:"record"`
	Module        string                   `json:"module,omitempty" field:"readonly" source:"record"`
	SourceHash    string                   `json:"sourceHash,omitempty" field:"readonly" source:"record"`
	BuildHash     string                   `json:"buildHash,omitempty" field:"readonly" source:"record"`
	BuiltSource   string                   `json:"builtSource,omitempty" field:"readonly" type:"longtext" source:"record"`
	BuiltLanguage string                   `json:"builtLanguage,omitempty" field:"readonly" source:"record"`
	BuiltInput    platform.ValueSchema     `json:"builtInput" field:"readonly,aside" type:"json"`
	BuiltOutput   platform.ValueSchema     `json:"builtOutput" field:"readonly,aside" type:"json"`
	Toolchain     string                   `json:"toolchain,omitempty" field:"readonly"`
	Diagnostics   string                   `json:"diagnostics,omitempty" field:"readonly" type:"longtext"`
	Version       int                      `json:"version,omitempty" field:"readonly"`
	Published     string                   `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions      []string                 `json:"versions,omitempty" field:"readonly"`
}

func (c Code) Definition() platform.Operation {
	abi := c.ABI
	if abi == "" {
		abi = platform.WasmCommandABI
	}
	return platform.Operation{Name: c.Name, Title: c.Title, Description: c.Description, Input: c.Input, Output: c.Output, Roles: c.Roles, Binding: platform.OperationBinding{Kind: "wasm", Module: c.Module, ABI: abi}, Limits: c.Limits}
}
func (c Code) sourceHash() string {
	h := sha256.Sum256([]byte(c.Source))
	return hex.EncodeToString(h[:])
}

func (c Code) buildHash() string {
	raw, _ := json.Marshal(platform.CodeBuildRequest{ABI: c.ABI, Language: c.Language, Source: c.Source, Input: c.Input, Output: c.Output})
	value, err := platform.DecodeValue(raw, 1<<20)
	if err != nil {
		return ""
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:])
}
func (b *Build) codeEntity() platform.Entity {
	return platform.Entity{Type: CodeType, Title: "Code function", Plural: "Code functions", Model: Code{}, Display: "title", Description: "A typed Go/TinyGo algorithm built in isolation and activated with its application.", Scope: platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}}, Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "compute"}, Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "compiling", Title: "Compiling", Tone: "warning"}, {Name: "compiled", Title: "Compiled", Tone: "success"}, {Name: "failed", Title: "Build failed", Tone: "danger"}, {Name: "published", Title: "Published", Tone: "success"}}, Transitions: []platform.Transition{{Name: "compile", Title: "Compile", Description: "Build the current source with an isolated pinned Go/TinyGo toolchain.", From: []string{"draft", "compiled", "failed", "published"}, To: []string{"compiling"}, Roles: []string{Builder}, Capability: "compute", Payload: []platform.Field{}, Do: b.compileCode}}}}
}
func (b *Build) compileCode(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	code, ok := record.(*Code)
	if !ok || code.Language != "go" && code.Language != "tinygo" || code.Input.Check() != nil || code.Output.Check() != nil || len(code.Source) == 0 || len(code.Source) > 256<<10 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Code requires Go/TinyGo source and bounded input/output schemas")
	}
	declaration := code.Definition()
	declaration.Binding.Module = strings.Repeat("0", 64)
	if err := declaration.Check(); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return nil
}

// compileCode's lifecycle decision receives its ChangeRecord in the shared
// generated action path. The request is planned by the owner's callback below.
func (b *Build) submitCodeCompile(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		code, ok := platform.Get[Code](c, s.GetTarget().GetId())
		if !ok || code.State == "compiling" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Code is missing or already compiling")
		}
		if err := b.compileCode(c, &code, nil, now); err != nil {
			return nil, err
		}
		return func(r *pb.ChangeRecord) {
			call, err := c.RequestCompilation(r, r.GetChangeId(), CodeType+"/"+code.ID, platform.CodeBuildRequest{ABI: code.ABI, Language: code.Language, Source: code.Source, Input: code.Input, Output: code.Output})
			if err != nil {
				return
			}
			code.State, code.Call, code.Diagnostics = "compiling", call.ID, ""
			c.Put(r, code)
		}, nil
	})
}
func codeActions() []platform.Action {
	return []platform.Action{{Schema: SchemaCodeCompiled, Target: CodeType, Capability: "compute", Title: "Keep code build result", Description: "Retain the accepted compiler output on its source asset.", Automation: true, Payload: []platform.Field{{Name: "call", Type: "string", Required: true, Description: "The accepted compilation call"}, {Name: "result", Type: "json", Required: true, Description: "Validated compiler result"}, {Name: "request", Type: "json", Required: true, Description: "The accepted source and SDK schema snapshot"}}}}
}
func (b *Build) submitCodeCompiled(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Call    string                    `json:"call"`
			Result  platform.CodeBuildResult  `json:"result"`
			Request platform.CodeBuildRequest `json:"request"`
		}
		code, ok := platform.Get[Code](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || code.Call != payload.Call || code.State != "compiling" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Compiler result has no matching current build")
		}
		return func(r *pb.ChangeRecord) {
			code.Diagnostics = payload.Result.Diagnostics
			code.State = "failed"
			built := Code{ABI: payload.Request.ABI, Language: payload.Request.Language, Source: payload.Request.Source, Input: payload.Request.Input, Output: payload.Request.Output}
			if payload.Result.Module != "" && payload.Result.SourceHash == built.sourceHash() && payload.Result.BuildHash == built.buildHash() {
				code.Module, code.SourceHash, code.Toolchain, code.BuildHash = payload.Result.Module, payload.Result.SourceHash, payload.Result.Toolchain, payload.Result.BuildHash
				code.BuiltABI = payload.Request.ABI
				code.BuiltSource, code.BuiltLanguage, code.BuiltInput, code.BuiltOutput = payload.Request.Source, payload.Request.Language, payload.Request.Input, payload.Request.Output
				code.State = "compiled"
			} else if code.Diagnostics == "" {
				code.Diagnostics = "Source or schema changed while this build was running"
			}
			c.Put(r, code)
		}, nil
	})
}
func (b *Build) Answer(c platform.Caller, effect platform.Effect, out platform.Outcome, now time.Time) *kernel.Error {
	if strings.HasPrefix(effect.Endpoint, WritebackEndpoint) {
		return b.answered(c, effect, out, now)
	}
	if effect.Endpoint != "operation" || !strings.HasPrefix(effect.Target, CodeType+"/") {
		return nil
	}
	var result platform.CodeBuildResult
	var binding struct {
		Build *platform.CodeBuildRequest `json:"build"`
	}
	if json.Unmarshal([]byte(effect.Body), &binding) != nil || binding.Build == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Compiler has no frozen source")
	}
	if out.Result == "delivered" {
		if err := json.Unmarshal(out.Answer, &result); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Compiler returned an invalid result")
		}
	} else {
		result.Diagnostics = out.Detail
	}
	payload, _ := json.Marshal(struct {
		Call    string                    `json:"call"`
		Result  platform.CodeBuildResult  `json:"result"`
		Request platform.CodeBuildRequest `json:"request"`
	}{effect.ID, result, *binding.Build})
	_, err := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "compiled:" + effect.ID, Target: &pb.EntityRef{Type: CodeType, Id: strings.TrimPrefix(effect.Target, CodeType+"/")}, Schema: &pb.SchemaRef{Name: SchemaCodeCompiled, Version: 1}, Payload: payload}, now)
	return err
}
func (b *Build) codeInventory() ([]Code, error) {
	return readDefinitionInventory[Code](b.host.Automation(platform.Caller{}, ID))
}
func (b *Build) checkCode(code Code) error {
	if code.Module == "" || code.SourceHash != code.sourceHash() || code.BuildHash != code.buildHash() || code.Language != "go" && code.Language != "tinygo" || code.Toolchain == "" {
		return fmt.Errorf("code %s needs an unchanged isolated build", code.Name)
	}
	if err := b.host.ValidateInstallOperation(platform.Caller{}, code.Definition()); err != nil {
		return err
	}
	list, err := b.codeInventory()
	if err != nil {
		return err
	}
	for _, other := range list {
		previous, _ := ReadPublication[Code](other.Published)
		if other.ID != code.ID && (other.Name == code.Name || previous.Name == code.Name) {
			return fmt.Errorf("compute %s is already declared", code.Name)
		}
	}
	if old, ok := ReadPublication[Code](code.Published); ok && old.Name != code.Name {
		return fmt.Errorf("a published compute keeps its name")
	}
	return nil
}
func (b *Build) installCode(c platform.Caller, code Code) error {
	if code.Published != "" {
		published, ok := ReadPublication[Code](code.Published)
		if !ok {
			return fmt.Errorf("compute has no valid publication")
		}
		code = published
	}
	if err := b.host.InstallOperation(c, code.Definition(), code.Version); err != nil {
		return err
	}
	b.codes[code.Name] = code
	return nil
}
func (b *Build) OperationDefinition(name string, version int) (platform.Operation, int, bool) {
	if current, ok := b.codes[name]; ok && (version == 0 || version == current.Version) {
		return current.Definition(), current.Version, true
	}
	if tb, ok := b.tables[name]; ok && (version == 0 || version == tb.Version) {
		return tb.Definition(), tb.Version, true
	}
	for _, tb := range b.tables {
		for _, raw := range tb.Versions {
			prior, ok := ReadPublication[Table](raw)
			if ok && prior.Name == name && prior.Version == version {
				return prior.Definition(), version, true
			}
		}
	}
	list, err := b.codeInventory()
	if err != nil {
		return platform.Operation{}, 0, false
	}
	for _, record := range list {
		if version < 1 || version > len(record.Versions) {
			continue
		}
		prior, ok := ReadPublication[Code](record.Versions[version-1])
		if ok && prior.Name == name && prior.Version == version {
			return prior.Definition(), version, true
		}
	}
	return platform.Operation{}, 0, false
}
func (b *Build) operationDeclarations() []platform.Operation {
	return append(definitionDeclarations(b.codes), definitionDeclarations(b.tables)...)
}
func codeImage(image []byte) (Code, error) {
	var code Code
	if json.Unmarshal(image, &code) != nil || !slices.Contains([]string{"draft", "published"}, code.State) || code.Version < 1 || code.Version > 64 || len(code.Versions) != code.Version || !slices.Contains(code.Versions, code.Published) {
		return code, fmt.Errorf("invalid compute publication family")
	}
	var selected Code
	for i, raw := range code.Versions {
		prior, ok := ReadPublication[Code](raw)
		if !ok || prior.ID != code.ID || prior.Name != code.Name || prior.Version != i+1 || prior.Definition().Check() != nil || prior.SourceHash != prior.sourceHash() || prior.Published != "" || len(prior.Versions) > 0 {
			return code, fmt.Errorf("invalid retained compute version")
		}
		if raw == code.Published {
			selected = prior
		}
	}
	selected.Versions = code.Versions
	return selected, nil
}
func codeReleaseAsset(code Code, manifest string) (platform.ReleaseAsset, error) {
	raw, err := json.Marshal(code.Definition())
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: code.Name}, ContractVersion: 1, SourceVersion: manifest + ".compute-" + strconv.Itoa(code.Version), Body: raw})
}
func (b *Build) CodeReleaseAssets() ([]platform.ReleaseAsset, error) {
	out := []platform.ReleaseAsset{}
	for _, op := range b.operationDeclarations() {
		if op.Binding.Kind == "native" {
			asset, err := tableReleaseAsset(b.tables[op.Name], b.Manifest().Version)
			if err != nil {
				return nil, err
			}
			out = append(out, asset)
			continue
		}
		asset, err := codeReleaseAsset(b.codes[op.Name], b.Manifest().Version)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}
func (b *Build) OperationReleaseAsset(name, version string) (platform.ReleaseAsset, error) {
	text, ok := strings.CutPrefix(version, b.Manifest().Version+".compute-")
	ordinal, err := strconv.Atoi(text)
	op, actual, found := b.OperationDefinition(name, ordinal)
	if !ok || err != nil || strconv.Itoa(ordinal) != text || !found || actual != ordinal || ordinal < 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("compute version is not retained")
	}
	raw, err := json.Marshal(op)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: name}, ContractVersion: 1, SourceVersion: version, Body: raw})
}
func (b *Build) InstallOperationAsset(asset platform.ReleaseAsset) error {
	var op platform.Operation
	text, ok := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+".compute-")
	ordinal, err := strconv.Atoi(text)
	if !ok || err != nil || ordinal < 1 || ordinal > 64 || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetCompute || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &op) != nil || op.Name != asset.Ref.Name || op.Check() != nil || platform.CheckAssetDependencies(asset) != nil {
		return fmt.Errorf("invalid compute candidate asset")
	}
	if op.Binding.Kind == "native" {
		table, err := tableFromAsset(asset)
		if err != nil {
			return err
		}
		return b.installTable(platform.Caller{Replaying: true}, table)
	}
	code := Code{Name: op.Name, Title: op.Title, Description: op.Description, Input: op.Input, Output: op.Output, Roles: op.Roles, Limits: op.Limits, Module: op.Binding.Module, State: "published", Version: ordinal}
	return b.installCode(platform.Caller{Replaying: true}, code)
}
func (b *Build) CodeDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	list, problem := b.codeInventory()
	if problem != nil {
		err = problem
		return
	}
	i := slices.IndexFunc(list, func(code Code) bool { return code.ID == id && !code.Archived })
	if i < 0 {
		err = fmt.Errorf("compute draft was not found")
		return
	}
	code := list[i]
	if old, ok := ReadPublication[Code](code.Published); ok {
		prior = platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: old.Name}
		hadPrior = true
	}
	next = platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: code.Name}
	if err = b.checkCode(code); err != nil {
		return
	}
	if code.Version >= 64 {
		err = fmt.Errorf("compute version family is full")
		return
	}
	code.Version++
	asset, problem := codeReleaseAsset(code, b.Manifest().Version)
	if problem != nil {
		err = problem
		return
	}
	after = slices.Clone(before)
	after = slices.DeleteFunc(after, func(a platform.ReleaseAsset) bool { return a.Ref == next || hadPrior && a.Ref == prior })
	after = append(after, asset)
	return
}

// PrepareCodeReleasePublications freezes the candidate's descriptor beside its
// mutable source draft. No new publish endpoint or standalone installation.
func (b *Build) PrepareCodeReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	list, err := b.codeInventory()
	if err != nil {
		return nil, err
	}
	out := []platform.ReleasePublication{}
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetCompute {
			continue
		}
		var operation platform.Operation
		if json.Unmarshal(asset.Body, &operation) == nil && operation.Binding.Kind == "native" {
			continue
		}
		i := slices.IndexFunc(list, func(code Code) bool { return code.Name == asset.Ref.Name && !code.Archived })
		if i < 0 {
			return nil, fmt.Errorf("compute has no source owner")
		}
		record := list[i]
		var op platform.Operation
		if json.Unmarshal(asset.Body, &op) != nil || op.Check() != nil {
			return nil, fmt.Errorf("compute descriptor is invalid")
		}
		text, ok := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+".compute-")
		ordinal, err := strconv.Atoi(text)
		if !ok || err != nil || ordinal < 1 || ordinal > 64 {
			return nil, fmt.Errorf("compute version is invalid")
		}
		if ordinal <= record.Version {
			retained, err := b.OperationReleaseAsset(asset.Ref.Name, asset.SourceVersion)
			if err != nil || !equalIntegrationAsset(retained, asset) {
				return nil, fmt.Errorf("retained compute bytes differ")
			}
			if len(record.Versions) != record.Version {
				return nil, fmt.Errorf("retained compute history is unavailable")
			}
			selected := record.Versions[ordinal-1]
			if selected == record.Published {
				continue
			}
			record.Published = selected
			if ordinal < record.Version {
				record.State = "draft"
			}
			out = append(out, platform.ReleasePublication{Schema: SchemaCodePublish, Image: platform.Raw(record)})
			continue
		}
		built := Code{ABI: record.BuiltABI, Language: record.BuiltLanguage, Source: record.BuiltSource, Input: record.BuiltInput, Output: record.BuiltOutput}
		if ordinal != record.Version+1 || record.Module != op.Binding.Module || record.SourceHash != built.sourceHash() || record.BuildHash != built.buildHash() {
			return nil, fmt.Errorf("compute source changed after candidate construction")
		}
		frozen := record
		frozen.ABI = record.BuiltABI
		frozen.Source, frozen.Language = record.BuiltSource, record.BuiltLanguage
		frozen.Input, frozen.Output, frozen.Roles, frozen.Limits = op.Input, op.Output, op.Roles, op.Limits
		frozen.Title, frozen.Description, frozen.Version, frozen.State = op.Title, op.Description, ordinal, "published"
		frozen.Published = published(frozen)
		record.Version, record.State, record.Published = ordinal, "published", frozen.Published
		record.Versions = append(record.Versions, record.Published)
		raw, _ := json.Marshal(record)
		out = append(out, platform.ReleasePublication{Schema: SchemaCodePublish, Image: raw})
	}
	return out, nil
}

func tableReleaseAsset(table Table, manifest string) (platform.ReleaseAsset, error) {
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: table.Name}, ContractVersion: 1, SourceVersion: manifest + ".compute-" + strconv.Itoa(table.Version), Body: platform.Raw(table.Definition())})
}

// A table's operation carries the complete rule plan. Reconstruct the editor
// columns from its ordered schema and its rows from that same plan.
func tableFromAsset(asset platform.ReleaseAsset) (Table, error) {
	var op platform.Operation
	text, found := strings.CutPrefix(asset.SourceVersion, ValueDefinitionVersion+".compute-")
	version, err := strconv.Atoi(text)
	if !found || err != nil || strconv.Itoa(version) != text || version < 1 || version > 64 || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetCompute || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &op) != nil || op.Name != asset.Ref.Name || op.Binding.Kind != "native" || op.Rules == nil || op.Check() != nil || platform.CheckAssetDependencies(asset) != nil {
		return Table{}, fmt.Errorf("invalid decision table asset %s", asset.Ref)
	}
	tb := Table{Name: op.Name, Title: op.Title, Description: op.Description, Reads: &op.Rules.Reads, State: "published", Version: version}
	columns := func(schema platform.ValueSchema) ([]TableColumn, error) {
		var columns []TableColumn
		for _, name := range schema.Required {
			field := schema.Properties[name]
			kind := map[string]string{"string": "text", "number": "number", "boolean": "boolean"}[field.Type]
			if kind == "" {
				return nil, fmt.Errorf("table column %s has an unsupported schema", name)
			}
			columns = append(columns, TableColumn{Name: name, Title: field.Description, Type: kind})
		}
		if len(columns) == 0 || len(columns) != len(schema.Properties) {
			return nil, fmt.Errorf("table columns need ordered, required schema fields")
		}
		return columns, nil
	}
	if tb.Inputs, err = columns(op.Input); err != nil {
		return Table{}, err
	}
	if tb.Outputs, err = columns(op.Output); err != nil {
		return Table{}, err
	}
	results := func(raw json.RawMessage) ([]string, error) {
		var values map[string]any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&values); err != nil {
			return nil, err
		}
		out := make([]string, 0, len(tb.Outputs))
		for _, col := range tb.Outputs {
			value, exists := values[col.Name]
			if !exists {
				return nil, fmt.Errorf("table result is missing %s", col.Name)
			}
			out = append(out, fmt.Sprint(value))
		}
		return out, nil
	}
	for _, row := range op.Rules.Rows {
		expressions := []platform.Expr{row.Condition}
		if len(tb.Inputs) > 1 {
			if row.Condition.Op != "and" || len(row.Condition.Args) != len(tb.Inputs) {
				return Table{}, fmt.Errorf("table row does not match its columns")
			}
			expressions = row.Condition.Args
		}
		then, err := results(row.Result)
		if err != nil {
			return Table{}, err
		}
		tb.Rows = append(tb.Rows, TableRow{Expressions: expressions, Then: then})
	}
	if len(op.Rules.Default) > 0 {
		if tb.Default, err = results(op.Rules.Default); err != nil {
			return Table{}, err
		}
	}
	if !bytes.Equal(platform.Raw(tb.Definition()), platform.Raw(op)) {
		return Table{}, fmt.Errorf("table operation cannot be represented by its original editor")
	}
	return tb, nil
}

func (b *Build) tableDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	tb, ok := platform.Get[Table](b.host.Automation(platform.Caller{}, ID), id)
	if !ok || tb.Archived {
		err = fmt.Errorf("decision table draft %s was not found", id)
		return
	}
	if old, ok := ReadPublication[Table](tb.Published); ok {
		prior, hadPrior = platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: old.Name}, true
	}
	if err = tb.NormalizeRules(); err == nil {
		err = b.checkTable(tb)
	}
	if err != nil {
		return
	}
	if tb.Version >= 64 {
		err = fmt.Errorf("table version family is full")
		return
	}
	tb.Version++
	next = platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: tb.Name}
	asset, problem := tableReleaseAsset(tb, b.Manifest().Version)
	if problem != nil {
		err = problem
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(asset platform.ReleaseAsset) bool { return asset.Ref == next || hadPrior && asset.Ref == prior })
	after = append(after, asset)
	return
}

func (b *Build) prepareTablePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	rows, err := readDefinitionInventory[Table](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return nil, err
	}
	var out []platform.ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetCompute {
			continue
		}
		var operation platform.Operation
		if json.Unmarshal(asset.Body, &operation) != nil || operation.Binding.Kind != "native" {
			continue
		}
		frozen, err := tableFromAsset(asset)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(tb Table) bool { return !tb.Archived && tb.Name == frozen.Name })
		if i < 0 {
			return nil, fmt.Errorf("table %s has no target draft", asset.Ref)
		}
		record := rows[i]
		if frozen.Version <= record.Version {
			retained, err := b.OperationReleaseAsset(asset.Ref.Name, asset.SourceVersion)
			if err != nil || !sameRuleAsset(retained, asset) {
				return nil, fmt.Errorf("retained table bytes differ")
			}
		} else if record.Version != 0 && frozen.Version != record.Version+1 {
			return nil, fmt.Errorf("table needs its next version or retained bytes")
		}
		frozen.Record = record.Record
		raw := published(frozen)
		for _, retained := range record.Versions {
			if prior, ok := ReadPublication[Table](retained); ok && prior.Version == frozen.Version {
				raw = retained
				break
			}
		}
		if !slices.Contains(record.Versions, raw) {
			record.Versions = append(slices.Clone(record.Versions), raw)
		}
		record.Version, record.State, record.Published = max(record.Version, frozen.Version), "published", raw
		out = append(out, platform.ReleasePublication{Schema: SchemaTablePublish, Image: platform.Raw(record)})
	}
	return out, nil
}

const CodeType = "build.code"
