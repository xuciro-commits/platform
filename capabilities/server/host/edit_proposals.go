package host

import (
	"context"
	"encoding/json"
	"net/http"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/objects"
)

type ApplyEditsRequest struct {
	Key  string `json:"key"`
	Step string `json:"step,omitempty"`
}

func (t *Tenant) readEditProposal(ctx context.Context, member platform.Member, engine, id, step string) (platform.EditProposal, *kernel.Error) {
	source := objects.EditProposalSource{
		Operation: func() (platform.OperationResult, *kernel.Error) { return t.ReadOperation(ctx, member, id) },
		AI:        func() (platform.OperationResult, *kernel.Error) { return t.ReadCapabilityAI(ctx, member, id, Now()) },
	}
	if engine == "flow" {
		return recordRead(t, ctx, func(t *Tenant) (platform.EditProposal, *kernel.Error) {
			source.FlowRun = func() (platform.FlowInstance, *kernel.Error) {
				view, refusal := t.recordOf(member, platform.FlowInstanceType, id, Now())
				if refusal != nil {
					return platform.FlowInstance{}, refusal
				}
				run, ok := view.Record.(platform.FlowInstance)
				if !ok {
					return platform.FlowInstance{}, nil
				}
				return run, nil
			}
			return source.Read(engine, step)
		})
	}
	return source.Read(engine, step)
}

func (h *Host) routesEditProposals(rt *routes) {
	rt.handle(transport.Route{Pattern: "GET /v1/capabilities/calls/{engine}/{id}/edits", Summary: "Review the original pure function's proposed actions without executing them", Answer: platform.EditProposal{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		proposal, refusal := t.readEditProposal(r.Context(), m, r.PathValue("engine"), r.PathValue("id"), r.URL.Query().Get("step"))
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		WriteJSON(w, http.StatusOK, proposal)
	})
	rt.handle(transport.Route{Pattern: "POST /v1/capabilities/calls/{engine}/{id}/edits", Summary: "Explicitly apply the original edit proposal through the normal authorized action engine", Body: ApplyEditsRequest{}, Answer: platform.CompositeAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request ApplyEditsRequest
		if err := transport.DecodeBodyLimit(r, &request, 4096); err != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a stable request key"))
			return
		}
		proposal, refusal := t.readEditProposal(r.Context(), m, r.PathValue("engine"), r.PathValue("id"), request.Step)
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		edits := make([]platform.CompositeEdit, 0, len(proposal.Edits))
		for _, edit := range proposal.Edits {
			if !json.Valid([]byte(edit.Payload)) {
				Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A proposed action needs JSON inputs"))
				return
			}
			revision := edit.Revision
			edits = append(edits, platform.CompositeEdit{Schema: edit.Schema, Target: &pb.EntityRef{Type: edit.Type, Id: edit.ID}, Payload: json.RawMessage(edit.Payload), ExpectedRevision: &revision})
		}
		answer, err := t.Composite(m, request.Key, edits, h.Now())
		if err != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error()))
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
}
