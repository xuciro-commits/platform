package host

import (
	"encoding/json"
	"net/http"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
)

type ApplyEditsRequest struct {
	Key string `json:"key"`
}

func (t *Tenant) readEditProposal(member platform.Member, engine, id string) (platform.EditProposal, *kernel.Error) {
	var result platform.OperationResult
	var refusal *kernel.Error
	switch engine {
	case "compute":
		result, refusal = t.ReadOperation(member, id)
	case "ai":
		result, refusal = t.ReadCapabilityAI(member, id, Now())
	default:
		return platform.EditProposal{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a function call")
	}
	if refusal != nil {
		return platform.EditProposal{}, refusal
	}
	if result.State != "completed" {
		return platform.EditProposal{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Wait for the function to finish before reviewing edits")
	}
	if err := platform.EditProposalSchema().Validate(result.Output, 48<<10); err != nil {
		return platform.EditProposal{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "This function did not return an edit proposal")
	}
	var proposal platform.EditProposal
	if err := json.Unmarshal(result.Output, &proposal); err != nil {
		return proposal, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return proposal, nil
}

func (h *Host) routesEditProposals(rt *routes) {
	rt.handle(transport.Route{Pattern: "GET /v1/capabilities/calls/{engine}/{id}/edits", Summary: "Review the original pure function's proposed actions without executing them", Answer: platform.EditProposal{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		proposal, refusal := t.readEditProposal(m, r.PathValue("engine"), r.PathValue("id"))
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		WriteJSON(w, http.StatusOK, proposal)
	})
	rt.handle(transport.Route{Pattern: "POST /v1/capabilities/calls/{engine}/{id}/edits", Summary: "Explicitly apply the original edit proposal through the normal authorized action engine", Body: ApplyEditsRequest{}, Answer: platform.CompositeAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request ApplyEditsRequest
		if err := decodeBodyLimit(r, &request, 4096); err != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a stable request key"))
			return
		}
		proposal, refusal := t.readEditProposal(m, r.PathValue("engine"), r.PathValue("id"))
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
