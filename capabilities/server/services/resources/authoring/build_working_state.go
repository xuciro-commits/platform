package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const WorkingStateType = "build.workingstate"
const SchemaWorkingStateSave = WorkingStateType + ".save"
const SchemaWorkingStateClear = WorkingStateType + ".clear"

type WorkingState struct {
	platform.Record
	Member   string                  `json:"member" field:"readonly"`
	Branch   string                  `json:"branch" field:"readonly"`
	Type     string                  `json:"type" field:"readonly"`
	Draft    string                  `json:"draft" field:"readonly"`
	Base     json.RawMessage         `json:"base,omitempty" field:"readonly" type:"json"`
	Spec     json.RawMessage         `json:"spec" field:"readonly" type:"json"`
	Inputs   json.RawMessage         `json:"inputs,omitempty" field:"readonly" type:"json"`
	Edit     *platform.CompositeEdit `json:"edit,omitempty" field:"readonly" type:"json"`
	Problems []string                `json:"problems,omitempty" field:"readonly"`
}

func WorkingStateID(member, branch, typ, draft string) string {
	digest := sha256.Sum256([]byte(member + "\x00" + branch + "\x00" + typ + "\x00" + draft))
	return hex.EncodeToString(digest[:])
}

func workingStateEntity() platform.Entity {
	return platform.Entity{Type: WorkingStateType, Title: "Working state", Model: WorkingState{}, Display: "draft", Scope: platform.Scope{Default: platform.ScopeOwn, Owner: "member"}}
}

func workingStateActions() []platform.Action {
	return []platform.Action{
		{Schema: SchemaWorkingStateSave, Target: WorkingStateType, Title: "Autosave working state", Description: "Keep this builder's current edits private on the selected branch.", Roles: []string{Builder}, Capability: "resources", Payload: []platform.Field{{Name: "branch", Type: "string", Required: true}, {Name: "type", Type: "string", Required: true}, {Name: "draft", Type: "string", Required: true}, {Name: "spec", Type: "json", Required: true}, {Name: "inputs", Type: "json"}, {Name: "base", Type: "json"}, {Name: "edit", Type: "json"}, {Name: "problems", Type: "string[]"}}},
		{Schema: SchemaWorkingStateClear, Target: WorkingStateType, Title: "Discard working state", Description: "Remove this builder's private edits after saving or discarding them.", Roles: []string{Builder}, Capability: "resources", Payload: []platform.Field{}},
	}
}

func (b *Build) submitWorkingState(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		id := s.GetTarget().GetId()
		state, exists := platform.Get[WorkingState](c, id)
		if exists && state.Member != c.ID {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The working state is unavailable")
		}
		if s.GetSchema().GetName() == SchemaWorkingStateClear {
			if !exists {
				return func(*pb.ChangeRecord) {}, nil
			}
			state.Spec, state.Inputs, state.Archived = nil, nil, true
		} else {
			var input WorkingState
			if json.Unmarshal(s.GetPayload(), &input) != nil || !BranchDraftType(input.Type) || input.Draft == "" || input.Branch == "" || len(input.Spec) > BranchMaxSpec || !json.Valid(input.Spec) || id != WorkingStateID(c.ID, input.Branch, input.Type, input.Draft) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A working state identifies its builder, branch and resource")
			}
			if input.Branch != "main" {
				branch, found := platform.Get[Branch](c, input.Branch)
				if !found || branch.State != BranchOpen {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Choose an open branch")
				}
			}
			state = WorkingState{Record: platform.Record{ID: id}, Member: c.ID, Branch: input.Branch, Type: input.Type, Draft: input.Draft, Spec: input.Spec, Inputs: input.Inputs, Base: input.Base, Edit: input.Edit, Problems: input.Problems}
		}
		return func(r *pb.ChangeRecord) { c.Put(r, state) }, nil
	})
}
