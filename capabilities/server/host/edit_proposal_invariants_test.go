package host

import (
	"encoding/json"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func TestInvariantEditProposalUsesNormalActionEngine(t *testing.T) {
	live := newInvariantTenant(t, "invariant-edit-proposal")
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})

	payload, _ := json.Marshal(map[string]any{"note": "accepted from proposal"})
	proposal := platform.EditProposal{Kind: platform.EditProposalVersion, Edits: []platform.ProposedAction{{
		Schema: "build.alpha.create", Type: "build.alpha", ID: "A1", Payload: string(payload), Revision: 0,
	}}}
	proposalJSON, _ := json.Marshal(proposal)
	live.accept(t, "build.table.create", build.TableType, "TABLE1", map[string]any{
		"name": "proposal", "title": "Create from proposal", "proposal": true,
		"inputs":  []build.TableColumn{{Name: "route", Type: "text"}},
		"outputs": []build.TableColumn{{Name: "proposal", Type: "text"}},
		"rows":    []build.TableRow{{When: []string{"go"}, Then: []string{string(proposalJSON)}}},
	})
	live.accept(t, build.SchemaTablePublish, build.TableType, "TABLE1", map[string]any{})

	now := invariantClock.Add(time.Minute)
	call, refusal := live.InvokeOperation(live.member(t), platform.OperationRequest{
		App: build.ID, Name: "proposal", Key: "proposal-call", Inputs: platform.Raw(map[string]any{"route": "go"}),
	}, now)
	if refusal != nil {
		t.Fatal(refusal)
	}
	for _, run := range live.operationDispatches(now) {
		run()
	}
	result, refusal := live.ReadOperation(t.Context(), live.member(t), call.ID)
	if refusal != nil || result.State != "completed" {
		t.Fatalf("proposal result state=%q refusal=%v", result.State, refusal)
	}
	read, refusal := live.readEditProposal(t.Context(), live.member(t), "compute", call.ID, "")
	if refusal != nil || len(read.Edits) != 1 || read.Edits[0].Type != "build.alpha" {
		t.Fatalf("read proposal: %+v refusal=%v", read, refusal)
	}
	edit := read.Edits[0]
	revision := edit.Revision
	answer, err := live.Composite(live.member(t), "proposal-apply", []platform.CompositeEdit{{
		Schema: edit.Schema, Target: &pb.EntityRef{Type: edit.Type, Id: edit.ID}, Payload: json.RawMessage(edit.Payload), ExpectedRevision: &revision,
	}}, now.Add(time.Second))
	if err != nil || answer.Applied != 1 {
		t.Fatalf("apply proposal: %+v: %v", answer, err)
	}
	view, refusal := live.RecordOf(t.Context(), live.member(t), "build.alpha", "A1", now.Add(2*time.Second))
	if refusal != nil || view.Values["note"] != "accepted from proposal" {
		t.Fatalf("created record=%+v refusal=%v", view, refusal)
	}
}
