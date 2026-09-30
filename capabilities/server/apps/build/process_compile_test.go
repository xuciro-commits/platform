package build

import (
	"encoding/json"
	"strings"
	"testing"

	"platformserver/platform"
)

func TestCompilerRejectsBranchOnlyAndEscapedIterationBindings(t *testing.T) {
	branchOnly := Process{Steps: []ProcessStep{{Name: "branch", Kind: "branch", Cases: map[string]string{"true": "yes", "false": "no"}}, {Name: "yes", Kind: "transform", Next: "merge"}, {Name: "no", Kind: "transform", Next: "merge"}, {Name: "merge", Kind: "end", Value: &platform.Binding{Source: "step", Step: "yes"}}}}
	if err := checkProcessScopes(branchOnly); err == nil || !strings.Contains(err.Error(), "not guaranteed") {
		t.Fatalf("optional branch output became a required downstream value: %v", err)
	}
	escaped := Process{Steps: []ProcessStep{{Name: "loop", Kind: "foreach", Body: "body", Next: "outside"}, {Name: "body", Kind: "transform", Next: "finish"}, {Name: "finish", Kind: "end"}, {Name: "outside", Kind: "end", Value: &platform.Binding{Source: "step", Step: "body"}}}}
	if err := checkProcessScopes(escaped); err == nil {
		t.Fatal("iteration-local output escaped without the loop's ordered result")
	}
}

func TestProcessStoredNodeDecodesOnceToCanonicalBindings(t *testing.T) {
	var process Process
	if err := json.Unmarshal([]byte(`{"name":"review","steps":[{"name":"ask","ask":"user","answers":["yes"],"branches":{"yes":"done"},"inputs":{"threshold":10}},{"name":"done","act":"close"}]}`), &process); err != nil {
		t.Fatal(err)
	}
	if process.Steps[0].Kind != "ask" || process.Steps[0].Cases["yes"] != "done" || process.Steps[0].Inputs["threshold"].Source != "literal" || process.Steps[1].Kind != "action" {
		t.Fatalf("stored node was not normalized: %+v", process)
	}
	canonical, _ := json.Marshal(process)
	if strings.Contains(string(canonical), `"branches"`) {
		t.Fatalf("legacy branch representation survived canonical output: %s", canonical)
	}
}

func TestRetainedLegacyProcessKeepsItsOriginalReleaseDefinition(t *testing.T) {
	raw := []byte(`{"id":"P","name":"review","title":"Review","object":"build.item","when":"open","steps":[{"name":"review","ask":"user","answers":["yes"],"branches":{"yes":"finish"}},{"name":"finish","act":"close"}],"version":1}`)
	var saved Process
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	asset, err := processReleaseAsset(saved, "1")
	if err != nil {
		t.Fatal(err)
	}
	var envelope platform.FlowReleaseDescriptor
	json.Unmarshal(asset.Body, &envelope)
	if strings.Contains(string(envelope.Definition), `"kind"`) || !strings.Contains(string(envelope.Definition), `"branches"`) {
		t.Fatalf("decoding changed retained release identity: %s", envelope.Definition)
	}
	if saved.Steps[0].Kind != "ask" || saved.Steps[0].Cases["yes"] != "finish" {
		t.Fatal("retained version did not compile through canonical nodes")
	}
	saved.Version = 2
	saved.originalDefinition = nil
	next, err := processReleaseAsset(saved, "1")
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(next.Body, &envelope)
	if !strings.Contains(string(envelope.Definition), `"kind"`) || strings.Contains(string(envelope.Definition), `"branches"`) {
		t.Fatal("new publication preserved the legacy editing representation")
	}
}
