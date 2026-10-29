package platformserver

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type parameterizedReadProbe struct {
	platform.App
	caller platform.Caller
	called string
	inputs json.RawMessage
}

func (p *parameterizedReadProbe) Manifest() platform.Manifest {
	return platform.Manifest{Reads: []string{"source", "legacy"}, ReadInputs: map[string][]platform.Field{
		"source": {{Name: "path", Type: "string", Required: true}, {Name: "offset", Type: "integer"}, {Name: "limit", Type: "integer"}},
	}}
}

func (p *parameterizedReadProbe) ReadInput(c platform.Caller, name string, inputs json.RawMessage) (any, *kernel.Error) {
	p.caller, p.called, p.inputs = c, name, inputs
	return "source result", nil
}
func (p *parameterizedReadProbe) Read(c platform.Caller, name string) (any, *kernel.Error) {
	p.caller, p.called = c, name
	return "legacy result", nil
}

func TestNamedReadInputsFollowTheOwnerDeclaration(t *testing.T) {
	app := &parameterizedReadProbe{}
	raw, refusal := namedReadInputs(app, "source", url.Values{
		"path": {"web/src/界面.tsx"}, "offset": {"12"}, "limit": {"40"}, "token": {"not a read input"},
	})
	if refusal != nil {
		t.Fatal(refusal)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"path": "web/src/界面.tsx", "offset": float64(12), "limit": float64(40)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inputs = %#v, want %#v", got, want)
	}
	for _, invalid := range []string{"1.5", "oops", "9223372036854775808"} {
		_, refusal := namedReadInputs(app, "source", url.Values{"offset": {invalid}})
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
			t.Fatalf("offset %q was not refused: %v", invalid, refusal)
		}
	}
}

func TestParameterizedReadPreservesCallerAndValidatesRequiredInput(t *testing.T) {
	app := &parameterizedReadProbe{}
	caller := platform.Caller{Member: platform.Member{ID: "builder"}, App: "system"}
	inputs := json.RawMessage(`{"path":"web/src/main.tsx","offset":12}`)
	out, refusal := platform.CallRead(app, caller, "source", inputs)
	if refusal != nil || out != "source result" || app.called != "source" || app.caller.ID != "builder" || app.caller.App != "system" || string(app.inputs) != string(inputs) {
		t.Fatalf("parameterized owner received caller=%#v inputs=%s; result=%v refusal=%v", app.caller, app.inputs, out, refusal)
	}
	for _, invalid := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`[]`), json.RawMessage(`null`)} {
		app.called = ""
		_, refusal := platform.CallRead(app, caller, "source", invalid)
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT || app.called != "" {
			t.Fatalf("invalid inputs %s reached the owner: called=%q refusal=%v", invalid, app.called, refusal)
		}
	}
}

func TestUnparameterizedReadKeepsItsOriginalOwner(t *testing.T) {
	app := &parameterizedReadProbe{}
	inputs, refusal := namedReadInputs(app, "legacy", url.Values{"path": {"ignored"}})
	if refusal != nil || inputs != nil {
		t.Fatalf("legacy read acquired inputs: %s %v", inputs, refusal)
	}
	out, refusal := platform.CallRead(app, platform.Caller{}, "legacy")
	if refusal != nil || out != "legacy result" || app.called != "legacy" {
		t.Fatalf("legacy result=%v called=%q refusal=%v", out, app.called, refusal)
	}
}

func TestNamedLiveReadFollowsItsDeclaringOwner(t *testing.T) {
	kit := newSystemTenant(t)
	before := kit.tenant.liveVersion("/v1/system-changes")
	kit.tenant.changedOwner("system")
	if after := kit.tenant.liveVersion("/v1/system-changes"); after == before {
		t.Fatal("system decision left its own live read stale")
	}
}
