package platformserver

import (
	"context"
	"encoding/json"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"slices"
	"time"
)

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
	if name == "packages" && !m.Holds(controlowner.PlatformApp, controlowner.Admin) && !m.Holds(controlowner.PlatformApp, platform.Auditor) {
		caller := t.caller(m, a, false)
		entry = caller.May("build", "build.app.create", []string{"builder"})
		if !m.InScope("build.app.create") {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
	}
	if refusal := authorizationRefusal(entry); refusal != nil {
		return nil, refusal
	}
	out, err := platform.CallRead(a, t.caller(m, a, false), name, inputs...)
	if err != nil {
		return nil, err
	}
	return t.narrowedSource(m, out, Now(), false, a.Manifest().ReadSources[name]), nil
}

func inputApp(app platform.App, c platform.Caller, name string, body []byte, at time.Time) (any, *kernel.Error) {
	if receiver, ok := app.(platform.InputReceiver); ok {
		return receiver.Input(c, name, body, at)
	}
	return nil, unknown()
}
