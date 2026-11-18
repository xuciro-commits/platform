package work

import (
	"encoding/json"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

func savedViewEntity() platform.RecordType {
	owner := func(record any) []string { return []string{record.(SavedView).Owner} }
	return platform.RecordType{Type: ViewType, Title: "Saved view", Model: SavedView{}, Scope: platform.Scope{
		Default: platform.ScopeOwn, Owner: "owner", Participants: owner,
		Share: func(record any, member string) (bool, string) {
			view := record.(SavedView)
			if view.Owner == member || view.Share == "tenant" {
				return true, ""
			}
			if view.Share == "project" {
				return false, view.Project
			}
			return false, ""
		},
	}}
}

func viewActions() []platform.Action {
	roles := []string{platform.AnyMember}
	return []platform.Action{
		{Schema: SchemaViewShare, Target: ViewType, Capability: "views", Title: "Share view", Description: "Change who can read the saved view definition.",
			Roles: roles, Payload: []platform.Field{
				{Name: "scope", Type: "string", Required: true, Choices: []string{"self", "project", "tenant"}, Description: "Who can read the view"},
				{Name: "project", Type: "string", Description: "Project ID for a project view"},
			}},
		{Schema: SchemaViewPreference, Target: ViewPreferenceType, New: true, Capability: "views", Title: "Set personal view preference",
			Description: "Pin a view or set it as your default without changing anyone else's preferences.", Roles: roles, Payload: []platform.Field{
				{Name: "view", Type: "string", Required: true, Description: "Saved view ID"},
				{Name: "pinned", Type: "boolean", Description: "Pin or unpin this view"},
				{Name: "default", Type: "boolean", Description: "Set or clear this view as your default"},
			}},
	}
}

func shareView(c platform.Caller, s *pb.Submission, view SavedView, exists bool, now time.Time) (func(*pb.ChangeRecord), *platform.Error) {
	if !exists || view.Archived {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	var input struct{ Scope, Project string }
	if json.Unmarshal(s.GetPayload(), &input) != nil {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if input.Scope == "project" {
		decision := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: protocol.AppType, Record: input.Project}, now)
		if input.Project == "" || !decision.Allow {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Choose a project you may read")
		}
	} else {
		input.Project = ""
	}
	view.Share, view.Project = input.Scope, input.Project
	return func(record *pb.ChangeRecord) { c.Put(record, view) }, nil
}

func (w *Work) viewPreference(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *platform.Error) {
	var input struct {
		View            string
		Pinned, Default *bool
	}
	if json.Unmarshal(s.GetPayload(), &input) != nil || input.Pinned == nil && input.Default == nil {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	view, exists := platform.Get[SavedView](c, input.View)
	decision := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: ViewType, Record: input.View}, now)
	if !exists || view.Archived || !decision.Allow {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	id := c.ID + "@" + view.Entity
	if id != s.GetTarget().GetId() {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	pref, _ := platform.Get[protocol.SavedViewPreference](c, id)
	pref.ID, pref.Member, pref.Entity = id, c.ID, view.Entity
	if input.Pinned != nil {
		pref.Pinned = slices.Clone(pref.Pinned)
		if *input.Pinned && !slices.Contains(pref.Pinned, view.ID) {
			pref.Pinned = append(pref.Pinned, view.ID)
		} else if !*input.Pinned {
			pref.Pinned = slices.DeleteFunc(pref.Pinned, func(known string) bool { return known == view.ID })
		}
	}
	if input.Default != nil {
		if *input.Default {
			pref.Default = view.ID
		} else if pref.Default == view.ID {
			pref.Default = ""
		}
	}
	return func(record *pb.ChangeRecord) { c.Put(record, pref) }, nil
}
