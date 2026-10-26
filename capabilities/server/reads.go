package platformserver

// Reads and connector inputs: the authorized read paths members call and the
// app-level helpers they route to.

import (
	"context"
	"encoding/json"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"time"
)

// journal records an accepted submission as the member's, for the audit and the journal.
// Input routes a connector input (push batch, poll page, heartbeat) to its app.
func (t *Tenant) Input(m platform.Member, name string, body []byte, now time.Time) (out any, err *kernel.Error) {
	if t.businessStopped() {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.businessStopped() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business access is stopped for maintenance")
	}
	if refusal := t.recordReadRefusal(); refusal != nil {
		return nil, refusal
	}
	a := t.owner["input:"+name]
	if a == nil {
		return nil, unknown()
	}
	if refusal := authorizationRefusal(t.authorizeEntry(m, a.Manifest().ID, "input:"+name, "action", now)); refusal != nil {
		t.refused(m.ID, name, refusal, now)
		return nil, refusal
	}
	end := t.begin("input "+name, trace.SpanContext{}, attribute.String("platform.app", a.Manifest().ID), attribute.String("platform.member", m.ID))
	defer func() { end(outcomeOf(err)) }()
	if a.Manifest().Inputs[name] {
		accepted, ok := a.(platform.AcceptedInputApp)
		if !ok || !slices.Contains(accepted.AcceptedInputs(), name) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT,
				"This connector input has no accepted-result boundary")
		}
		return t.inputAccepted(accepted, m, name, body, now)
	}
	defer t.enqueue(now)
	out, err = inputApp(a, t.caller(m, a, false), name, body, now)
	if err != nil {
		t.refused(m.ID, name, err, now)
	}
	return out, err
}

// Read serves a named read of the app that declares it, to members holding a
// role in that app or to everyone when the manifest says so; the app may refuse further.
func (t *Tenant) Read(m platform.Member, name string, inputs ...json.RawMessage) (any, *kernel.Error) {
	if name == "health" && t.quarantined() {
		t.mu.Lock()
		defer t.mu.Unlock()
		return t.read(m, name, inputs...)
	}
	return coordinatedRecordRead(t, context.Background(), func() (any, *kernel.Error) { return t.read(m, name, inputs...) })
}
func (t *Tenant) read(m platform.Member, name string, inputs ...json.RawMessage) (any, *kernel.Error) {
	return t.readFrom(m, t.owner["read:"+name], name, inputs...)
}

// ReadFrom keeps a composed form's reads on the action's declared owner.
// Authorization and result projection stay on Read's path.
func (t *Tenant) ReadFrom(m platform.Member, app, name string, inputs ...json.RawMessage) (any, *kernel.Error) {
	return coordinatedRecordRead(t, context.Background(), func() (any, *kernel.Error) { return t.readFromApp(m, app, name, inputs...) })
}
func (t *Tenant) readFromApp(m platform.Member, app, name string, inputs ...json.RawMessage) (any, *kernel.Error) {
	a := t.app(app)
	if a == nil || !slices.Contains(a.Manifest().Reads, name) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return t.readFrom(m, a, name, inputs...)
}

func (t *Tenant) readFrom(m platform.Member, a platform.App, name string, inputs ...json.RawMessage) (any, *kernel.Error) {
	m = t.authorizationMember(m)
	if err := t.admits(m); err != nil {
		return nil, err
	}
	if a == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	entry := t.authorizeEntry(m, a.Manifest().ID, name, "read", Now())
	if name == "packages" && !m.Holds(PlatformApp, Admin) && !m.Holds(PlatformApp, Auditor) {
		caller := t.caller(m, a, false)
		entry = caller.May("build", "build.app.create", []string{"builder"})
		if !m.InScope("build.app.create") {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
	}
	if refusal := authorizationRefusal(entry); refusal != nil {
		return nil, refusal
	}
	out, err := readApp(a, t.caller(m, a, false), name, inputs...)
	if err != nil {
		return nil, err
	}
	// An app chooses which records its read answers with; the host keeps what
	// leaves no wider than the member's authority over the fields and sources
	// behind them (#130), however the app collected them.
	return t.narrowedSource(m, out, Now(), false, a.Manifest().ReadSources[name]), nil
}

func readApp(app platform.App, c platform.Caller, name string, inputs ...json.RawMessage) (any, *kernel.Error) {
	if app == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	// Named reads are optional: an app implementing neither reader answers
	// NOT_FOUND without building a manifest it may not be able to build.
	parameterized, hasParameterized := app.(platform.ParameterizedReader)
	_, hasPlain := app.(platform.Reader)
	if !hasParameterized && !hasPlain {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if fields, declared := app.Manifest().ReadInputs[name]; declared {
		if reader, ok := parameterized, hasParameterized; ok {
			raw := json.RawMessage(`{}`)
			if len(inputs) > 0 && len(inputs[0]) > 0 {
				raw = inputs[0]
			}
			var values map[string]any
			if json.Unmarshal(raw, &values) != nil || values == nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Read inputs must be a JSON object")
			}
			if issues := platform.InputIssues(fields, values); len(issues) > 0 {
				return nil, c.RefuseFields(issues)
			}
			return reader.ReadInput(c, name, raw)
		}
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if reader, ok := app.(platform.Reader); ok {
		return reader.Read(c, name)
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}

func inputApp(app platform.App, c platform.Caller, name string, body []byte, at time.Time) (any, *kernel.Error) {
	if receiver, ok := app.(platform.InputReceiver); ok {
		return receiver.Input(c, name, body, at)
	}
	return nil, unknown()
}
