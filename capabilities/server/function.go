package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// The accepted effect owns the complete bounded definition and input. Dispatch
// must never obtain a newer prompt/schema from the current manifest.
type functionBinding struct {
	Definition platform.AIFunction   `json:"definition"`
	Call       platform.FunctionCall `json:"call"`
	Member     string                `json:"member"`
}

func (d *stagedDecision) RequestFunction(c platform.Caller, r *pb.ChangeRecord, request platform.FunctionRequest) (platform.FunctionCall, *kernel.Error) {
	call, effect, refusal := d.tenant.planFunction(c, r, request, d.records, d.intents)
	if refusal != nil {
		if d.failure == nil {
			d.failure = refusal
		}
		return platform.FunctionCall{}, refusal
	}
	if !d.probing {
		d.intents = append(d.intents, effect)
	}
	return call, nil
}

func (t *Tenant) planFunction(c platform.Caller, r *pb.ChangeRecord, request platform.FunctionRequest, store *recordStore, pending []platform.Effect) (platform.FunctionCall, platform.Effect, *kernel.Error) {
	refuse := func(code pb.ErrorCode, message string) (platform.FunctionCall, platform.Effect, *kernel.Error) {
		return platform.FunctionCall{}, platform.Effect{}, platform.Refuse(code, message)
	}
	if c.Tenant != t.ID || r.GetSubmission().GetTenantId() != t.ID || r.GetSubmission().GetAuthority() != c.App ||
		request.Version < 0 || c.Automation && request.OnBehalf == "" || !c.Automation && (request.Release != nil || request.OnBehalf != "" && request.OnBehalf != c.ID) {
		return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "AI functions need an explicit authorised member")
	}
	member := c.Member
	if c.Automation {
		var found bool
		member, found = t.Member(request.OnBehalf)
		if !found || member.Tenant != t.ID || member.Agent {
			return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "AI functions need an explicit authorised member")
		}
	}
	app := t.app(c.App)
	if app == nil {
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The AI function is not declared")
	}
	manifest := app.Manifest()
	f, version, found := functionDefinition(app, request.Name, request.Version)
	if !found {
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The AI function is not declared")
	}
	if f.Check() != nil || !slices.Contains(f.Roles, member.Roles[c.App]) {
		return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This member cannot call the AI function")
	}
	target := r.GetSubmission().GetTarget()
	action, ok := manifest.Actions.Action(request.Reply)
	if !ok || action.Target != target.GetType() || t.authorityOf(target.GetType()) != c.App || action.Approval != nil || !action.Automation {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The AI function needs an automatic reply on its owner's target object")
	}
	source := request.Source
	if source == "" && target.GetType() == f.Object {
		source = target.GetId()
	}
	if source == "" {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an AI function source record")
	}
	sourceRef := f.Object + "/" + source
	at := r.GetRecordedTime().AsTime()
	store.mu.Lock()
	et := store.types[f.Object]
	if et == nil {
		store.mu.Unlock()
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The AI function source is not readable")
	}
	row := et.rows[source]
	if row == nil || recordOf(row.value).Archived || !t.readableIn(store, member, sourceRef, at) {
		store.mu.Unlock()
		return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The AI function source is not readable")
	}
	input := map[string]any{}
	sources := []string{}
	for _, name := range f.Fields {
		field, found := et.info.Field(name)
		if !found || !field.Reads(member.Roles[et.info.App]) {
			store.mu.Unlock()
			return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The AI function source is not readable")
		}
		input[name] = row.value.FieldByIndex(field.Index).Interface()
		sources = append(sources, sourceRef+"#"+name)
	}
	raw, err := json.Marshal(input)
	store.mu.Unlock()
	if err != nil || len(raw) > f.MaxInputBytes {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The AI function input exceeds its byte budget")
	}
	definition, err := canonicalDigest([]any{c.App, f})
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The AI function definition cannot be bound")
	}
	candidate, release, err := t.functionClosure(c.App, f, version, request.Release)
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The AI function dependencies cannot be bound")
	}
	hash, _ := canonicalDigest(string(raw))
	q := platform.Request{Model: f.Model, Target: target.GetId(), Reply: request.Reply,
		Payload: platform.Prompt{System: f.SystemPrompt(), User: string(raw), MaxTokens: f.MaxTokens}}
	x := t.planModelRequest(c, r, q, pending)
	var ask modelAsk
	_ = json.Unmarshal([]byte(x.Body), &ask)
	call := platform.FunctionCall{Definition: definition, Dependencies: candidate.ID, Model: ask.Model, InputHash: hash, Sources: sources, Source: sourceRef, Version: version, Release: release}
	if ask.Model == "" {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The AI function needs a bound model")
	}
	ask.Function = &functionBinding{Definition: f, Call: call, Member: member.ID}
	body, _ := json.Marshal(ask)
	x.Body = string(body)
	return call, x, nil
}

// Recheck current grants before releasing saved input to the provider. This
// checks permissions only; it does not replace accepted input with live data.
func (t *Tenant) functionAllowed(app string, ask modelAsk, at time.Time) bool {
	binding := ask.Function
	f := binding.Definition
	definition, err := canonicalDigest([]any{app, f})
	hash, hashErr := canonicalDigest(ask.Prompt.User)
	member, ok := t.Member(binding.Member)
	sourceType, sourceID, sourceOK := strings.Cut(binding.Call.Source, "/")
	replyType, replyID, replyOK := strings.Cut(ask.Record, "/")
	if !ok || member.Tenant != t.ID || f.Check() != nil || err != nil || hashErr != nil ||
		definition != binding.Call.Definition || hash != binding.Call.InputHash || ask.Model != binding.Call.Model ||
		ask.Prompt.System != f.SystemPrompt() || ask.Prompt.MaxTokens != f.MaxTokens || len(ask.Prompt.User) > f.MaxInputBytes ||
		!sourceOK || sourceID == "" || sourceType != f.Object || !replyOK || replyID != ask.Call || t.authorityOf(replyType) != app || !slices.Contains(f.Roles, member.Roles[app]) ||
		member.Agent && t.suspended(member.ID) || len(binding.Call.Sources) != len(f.Fields) {
		return false
	}
	may := t.mayRead(member, at, false)
	for i, field := range f.Fields {
		ref := binding.Call.Source + "#" + field
		if binding.Call.Sources[i] != ref || !may(ref) {
			return false
		}
	}
	return true
}

func (h hostView) ValidateInstallFunction(f platform.AIFunction) error {
	if err := f.Check(); err != nil {
		return err
	}
	manifest := h.app.Manifest()
	h.t.records.mu.Lock()
	defer h.t.records.mu.Unlock()
	et := h.t.records.types[f.Object]
	if et == nil || et.info.App != manifest.ID {
		return fmt.Errorf("function %s needs its owner's source object", f.Name)
	}
	for _, role := range f.Roles {
		if !slices.Contains(manifest.AllRoles(), role) {
			return fmt.Errorf("function %s names an undeclared role", f.Name)
		}
	}
	for _, name := range f.Fields {
		field, ok := et.info.Field(name)
		if !ok || !slices.Contains([]string{"text", "longtext", "choice", "integer", "decimal", "boolean"}, field.Type) || slices.Contains(h.t.narrowable(et), name) {
			return fmt.Errorf("function %s needs direct scalar source fields", f.Name)
		}
	}
	return nil
}

func (h hostView) InstallFunction(c platform.Caller, f platform.AIFunction, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if version < 1 {
		return fmt.Errorf("function %s needs a published version", f.Name)
	}
	if err := h.ValidateInstallFunction(f); err != nil {
		return err
	}
	ref := platform.AssetRef{App: h.app.Manifest().ID, Kind: platform.AssetFunction, Name: f.Name}
	def := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".function-" + strconv.Itoa(version), ContractVersion: 1, Requires: []platform.AssetRef{{App: ref.App, Kind: platform.AssetObject, Name: f.Object}}, Function: &f}
	i := slices.IndexFunc(h.t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if i >= 0 {
		if h.t.definitions[i].Source != "tenant" {
			return fmt.Errorf("function %s is owned by code", ref)
		}
		h.t.definitions[i] = def
	} else {
		h.t.definitions = append(h.t.definitions, def)
	}
	// An installed declaration may grant an existing code action to newly
	// declared tenant roles. Keep discovery consistent with the owner's catalog.
	for i := range h.t.definitions {
		d := &h.t.definitions[i]
		if d.Ref.App == ref.App && d.Source == "code" && d.Action != nil {
			if action, ok := h.app.Manifest().Actions.Action(d.Ref.Name); ok {
				d.Action = &action
			}
		}
	}
	slices.SortFunc(h.t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return nil
}

// Only the definition owner can resolve retained versions. Code declarations
// have their manifest version and cannot invent a native publication ordinal.
func functionDefinition(app platform.App, name string, version int) (platform.AIFunction, int, bool) {
	if owner, ok := app.(interface {
		FunctionDefinition(string, int) (platform.AIFunction, int, bool)
	}); ok {
		return owner.FunctionDefinition(name, version)
	}
	if version != 0 {
		return platform.AIFunction{}, 0, false
	}
	for _, f := range app.Manifest().Functions {
		if f.Name == name {
			return f, 0, true
		}
	}
	return platform.AIFunction{}, 0, false
}

func (t *Tenant) functionClosure(app string, f platform.AIFunction, version int, retained *string) (platform.ReleaseCandidate, string, error) {
	ref := platform.AssetRef{App: app, Kind: platform.AssetFunction, Name: f.Name}
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	i := slices.IndexFunc(available, func(a platform.ReleaseAsset) bool { return a.Ref == ref })
	if i < 0 {
		return platform.ReleaseCandidate{}, "", fmt.Errorf("function %s has no release owner", ref)
	}
	body, err := json.Marshal(f)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	available[i].Body = body
	if version > 0 {
		available[i].SourceVersion = t.app(app).Manifest().Version + ".function-" + strconv.Itoa(version)
	}
	candidate, err := platform.Candidate([]platform.AssetRef{ref}, available)
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	release := t.activeRelease
	if retained != nil {
		release = *retained
	}
	if release == "" {
		return candidate, "", nil
	}
	saved, err := platform.ReadCandidate(release, t.releaseCandidates[release])
	if err != nil {
		return platform.ReleaseCandidate{}, "", err
	}
	if !slices.ContainsFunc(saved.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref }) {
		if retained != nil {
			return platform.ReleaseCandidate{}, "", fmt.Errorf("function %s is missing from its retained release", ref)
		}
		return candidate, "", nil
	}
	active, err := platform.Candidate([]platform.AssetRef{ref}, saved.Assets)
	if err != nil || active.ID != candidate.ID {
		return platform.ReleaseCandidate{}, "", fmt.Errorf("function %s differs from its active release", ref)
	}
	return candidate, release, nil
}
