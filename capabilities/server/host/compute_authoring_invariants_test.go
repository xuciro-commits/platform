package host

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	functionauthoring "platformserver/services/functions/authoring"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK1ComputeAuthoringRetainsVersions(t *testing.T) {
	live := newInvariantTenant(t, "invariant-compute-authoring")
	live.accept(t, "build.table.create", functionauthoring.TableType, "TABLE", map[string]any{
		"name": "sizing", "title": "Sizing", "inputs": []functionauthoring.TableColumn{{Name: "amount", Type: "number"}},
		"outputs": []functionauthoring.TableColumn{{Name: "result", Type: "text"}},
		"rows":    []functionauthoring.TableRow{{When: []string{">=10"}, Then: []string{"large"}}}, "default": []string{"small"},
	})
	live.accept(t, "build.table.publish", functionauthoring.TableType, "TABLE", map[string]any{})
	owner := live.App(build.ID).(*build.Build)
	ledger, err := owner.Authority().ForkRead()
	if err != nil {
		t.Fatal(err)
	}
	fork, err := owner.Fork(ledger)
	if err != nil {
		t.Fatal(err)
	}
	apps := slices.Clone(live.apps)
	apps[live.appIndex[build.ID]] = fork
	forkView := &Tenant{ownerSet: live.with(apps)}
	live.accept(t, "build.table.edit", functionauthoring.TableType, "TABLE", map[string]any{"title": "Sizing v2"})
	preview, err := owner.DraftReleaseAssetsMulti([]build.JointDraftRef{{Kind: platform.AssetCompute, ID: "TABLE"}}, live.member(t), false)
	if err != nil {
		t.Fatal(err)
	}
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetCompute, Name: "sizing"}
	var candidate platform.ReleaseAsset
	for _, asset := range preview.After {
		if asset.Ref == ref {
			candidate = asset
		}
	}
	var candidateOperation platform.Operation
	if candidate.SourceVersion != "1.compute-2" || json.Unmarshal(candidate.Body, &candidateOperation) != nil || candidateOperation.Title != "Sizing v2" {
		t.Fatalf("candidate lost the edited table: %+v", candidate)
	}
	live.accept(t, "build.table.publish", functionauthoring.TableType, "TABLE", map[string]any{})

	operation := func(view *Tenant, requested, expected int, title string) platform.Operation {
		t.Helper()
		definition, version, ok := (hostView{t: view}).Operation(build.ID, "sizing", requested)
		if !ok || version != expected || definition.Title != title {
			t.Fatalf("operation %d: version=%d found=%t definition=%+v", requested, version, ok, definition)
		}
		return definition
	}
	first := operation(live.Tenant, 1, 1, "Sizing")
	second := operation(live.Tenant, 0, 2, "Sizing v2")
	operation(forkView, 0, 1, "Sizing")

	archive := func(typ, id string) {
		t.Helper()
		_, refusal := live.Submit(live.member(t), live.submission(typ+".archive", typ, id, map[string]any{}), invariantClock)
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
			t.Fatalf("archive %s/%s: %v", typ, id, refusal)
		}
	}
	archive(functionauthoring.TableType, "TABLE")
	live.accept(t, "build.code.create", functionauthoring.CodeType, "CODE", map[string]any{
		"name": "algorithm", "title": "Algorithm", "description": "A code function", "language": "go", "source": "package main\nfunc main() {}",
		"input": platform.ValueSchema{Type: "string"}, "output": platform.ValueSchema{Type: "string"}, "roles": []string{build.Builder},
		"limits": platform.OperationLimits{TimeoutMillis: 100, MemoryPages: 1, MaxInputBytes: 1024, MaxOutputBytes: 1024},
	})
	live.accept(t, "build.code.compile", functionauthoring.CodeType, "CODE", map[string]any{})
	archive(functionauthoring.CodeType, "CODE")

	replay := live.restore(t, live.entries)
	if restored := operation(replay.Tenant, 1, 1, "Sizing"); !bytes.Equal(platform.Raw(first), platform.Raw(restored)) {
		t.Fatal("replay changed the retained version")
	}
	if restored := operation(replay.Tenant, 0, 2, "Sizing v2"); !bytes.Equal(platform.Raw(second), platform.Raw(restored)) {
		t.Fatal("replay changed the installed version")
	}
}
