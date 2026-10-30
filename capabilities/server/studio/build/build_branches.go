package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
)

const BranchType = "build.branch"

const (
	BranchOpen     = "open"
	BranchProposed = "proposed"
	BranchMerged   = "merged"
	BranchClosed   = "closed"

	BranchMaxChanges  = 512
	BranchMaxSpec     = 512 << 10
	BranchMaxBytes    = 8 << 20
	BranchMaxReviews  = 64
	BranchMaxChecks   = 256 << 10
	BranchMaxReviewer = 16
)

type Branch struct {
	platform.Record
	Name        string          `json:"name" field:"required" help:"Lower-case letters and digits" example:"q4pricing"`
	Title       string          `json:"title" field:"required" title:"What people call it" example:"Q4 pricing rework"`
	Description string          `json:"description,omitempty" type:"longtext" help:"What this branch changes and why"`
	State       string          `json:"state" field:"readonly"`
	Changes     []BranchChange  `json:"changes" type:"json" field:"readonly" title:"Change set"`
	Proposal    *BranchProposal `json:"proposal,omitempty" type:"json" field:"readonly"`
	Merge       *BranchMerge    `json:"merge,omitempty" type:"json" field:"readonly"`
}

type BranchChange struct {
	Seq     int             `json:"seq"`
	At      time.Time       `json:"at"`
	By      string          `json:"by"`
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Op      string          `json:"op" enum:"create,update,delete"`
	HasBase bool            `json:"hasBase,omitempty"`
	Base    json.RawMessage `json:"base,omitempty"`
	Spec    json.RawMessage `json:"spec,omitempty"`
	Rebased bool            `json:"rebased,omitempty"`
}

type BranchReview struct {
	By       string    `json:"by"`
	At       time.Time `json:"at"`
	Decision string    `json:"decision" enum:"approve,changes"`
	Note     string    `json:"note,omitempty"`
}

type BranchProposal struct {
	By        string          `json:"by"`
	At        time.Time       `json:"at"`
	Note      string          `json:"note,omitempty"`
	Reviewers []string        `json:"reviewers"`
	Reviews   []BranchReview  `json:"reviews"`
	Checks    json.RawMessage `json:"checks,omitempty"`
}

type BranchMerge struct {
	By        string          `json:"by"`
	At        time.Time       `json:"at"`
	Candidate string          `json:"candidate,omitempty"`
	Applied   json.RawMessage `json:"applied,omitempty"`
}

func BranchDraftType(typ string) bool {
	_, err := draftAssetRef(typ, "")
	return err == nil
}

func BranchResource(typ, name string) (platform.AssetRef, error) { return draftAssetRef(typ, name) }

func (b Branch) Resources() []string {
	var out []string
	for _, change := range b.Changes {
		key := change.Type + "/" + change.ID
		if !slices.Contains(out, key) {
			out = append(out, key)
		}
	}
	return out
}

func (b Branch) Latest(typ, id string) (BranchChange, bool) {
	for i := len(b.Changes) - 1; i >= 0; i-- {
		if b.Changes[i].Type == typ && b.Changes[i].ID == id {
			return b.Changes[i], true
		}
	}
	return BranchChange{}, false
}

func (b Branch) Base(typ, id string) (json.RawMessage, bool) {
	for i := len(b.Changes) - 1; i >= 0; i-- {
		if c := b.Changes[i]; c.Type == typ && c.ID == id && c.HasBase {
			return c.Base, true
		}
	}
	return nil, false
}

func (b *Build) branchEntity() platform.Entity {
	builder := []string{Builder}
	return platform.Entity{Type: BranchType, Title: "Branch", Plural: "Branches", Model: Branch{}, Display: "title",
		Description: "A named overlay of resource drafts: saves append to its change set, instance data is never part of it, and a reviewed proposal merges it into the drafts as one release.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: builder, Capability: "branches"},
		Validate: func(c platform.Caller, record any) *kernel.Error {
			branch := record.(*Branch)
			if !Named(branch.Name) || len(branch.Name) > 64 || len(branch.Title) > 200 || len(branch.Description) > 4096 {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A branch name is lower-case letters and digits, 64 at most")
			}
			return nil
		},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: BranchOpen,
			States: []platform.State{{Name: BranchOpen, Title: "Open", Tone: "info"}, {Name: BranchProposed, Title: "Proposed", Tone: "warning"}, {Name: BranchMerged, Title: "Merged", Tone: "success"}, {Name: BranchClosed, Title: "Closed", Tone: "neutral"}},
			Transitions: []platform.Transition{
				{Name: "save", Title: "Save into branch", Description: "Append a resource's saved spec to the change set.", From: []string{BranchOpen}, To: []string{BranchOpen}, Roles: builder, Capability: "branches",
					Payload: []platform.Field{{Name: "type", Type: "string", Required: true, Description: "The resource draft type"}, {Name: "id", Type: "string", Required: true, Description: "The resource draft id"}, {Name: "op", Type: "string", Required: true, Choices: []string{"create", "update", "delete"}, Description: "What the branch does to the resource"}, {Name: "base", Type: "json", Description: "The main spec this change builds on; null when the resource is new"}, {Name: "spec", Type: "json", Description: "The full saved spec"}, {Name: "rebased", Type: "boolean", Description: "The base moved forward to a newer main"}},
					Do:      saveBranchChange},
				{Name: "propose", Title: "Propose", Description: "Ask reviewers to approve merging the branch.", From: []string{BranchOpen}, To: []string{BranchProposed}, Roles: builder, Capability: "branches",
					Payload: []platform.Field{{Name: "reviewers", Type: "json", Description: "Builders who review"}, {Name: "note", Type: "string", Description: "What the reviewers should know"}, {Name: "checks", Type: "json", Description: "The merge checks at proposal time"}},
					Do:      proposeBranch},
				{Name: "review", Title: "Review", Description: "A named reviewer approves or asks for changes.", From: []string{BranchProposed}, To: []string{BranchProposed}, Roles: builder, Capability: "branches",
					Payload: []platform.Field{{Name: "decision", Type: "string", Required: true, Choices: []string{"approve", "changes"}, Description: "The decision"}, {Name: "note", Type: "string", Description: "Why"}},
					Do:      reviewBranch},
				{Name: "withdraw", Title: "Withdraw proposal", Description: "Return a proposed branch to editing.", From: []string{BranchProposed}, To: []string{BranchOpen}, Roles: builder, Capability: "branches", Payload: []platform.Field{},
					Do: func(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
						record.(*Branch).Proposal = nil
						return nil
					}},
				{Name: "merge", Title: "Merge", Description: "Record that the reviewed branch was merged into the drafts as one release.", From: []string{BranchProposed}, To: []string{BranchMerged}, Roles: builder, Capability: "branches",
					Payload: []platform.Field{{Name: "candidate", Type: "string", Description: "The release candidate the merge produced"}, {Name: "applied", Type: "json", Description: "The resources the merge changed"}},
					Do:      mergeBranch},
				{Name: "close", Title: "Close", Description: "Abandon the branch.", From: []string{BranchOpen, BranchProposed}, To: []string{BranchClosed}, Roles: builder, Capability: "branches", Payload: []platform.Field{}}}}}
}

func branchInvalid(message string) *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message)
}

func saveBranchChange(c platform.Caller, record any, payload json.RawMessage, at time.Time) *kernel.Error {
	branch := record.(*Branch)
	var raw map[string]json.RawMessage
	var change BranchChange
	if json.Unmarshal(payload, &raw) != nil || json.Unmarshal(payload, &change) != nil {
		return branchInvalid("A branch change is one JSON object")
	}
	_, change.HasBase = raw["base"]
	if !BranchDraftType(change.Type) || change.ID == "" || len(change.ID) > 256 || !slices.Contains([]string{"create", "update", "delete"}, change.Op) {
		return branchInvalid("A branch change names a resource draft type, its id and create, update or delete")
	}
	if change.Op != "delete" {
		var object map[string]json.RawMessage
		if json.Unmarshal(change.Spec, &object) != nil || object == nil || len(change.Spec) > BranchMaxSpec {
			return branchInvalid("A saved spec is one JSON object within the size limit")
		}
	} else {
		change.Spec = nil
	}
	_, known := branch.Base(change.Type, change.ID)
	if !known && !change.HasBase || change.Rebased && !change.HasBase || change.Op == "create" && change.HasBase && !bytes.Equal(bytes.TrimSpace(change.Base), []byte("null")) {
		return branchInvalid("The first change to a resource names the main spec it builds on")
	}
	if change.HasBase && len(change.Base) > BranchMaxSpec {
		return branchInvalid("A base spec is within the size limit")
	}
	if len(branch.Changes) >= BranchMaxChanges {
		return branchInvalid("A branch keeps at most 512 changes; merge or close it")
	}
	total := len(change.Spec) + len(change.Base)
	for _, existing := range branch.Changes {
		total += len(existing.Spec) + len(existing.Base)
	}
	if total > BranchMaxBytes {
		return branchInvalid("The branch change set exceeds its size limit")
	}
	if !change.HasBase {
		change.Base = nil
	}
	change.Seq, change.At, change.By = len(branch.Changes)+1, at.UTC(), c.ID
	branch.Changes = append(branch.Changes, change)
	return nil
}

func proposeBranch(c platform.Caller, record any, payload json.RawMessage, at time.Time) *kernel.Error {
	branch := record.(*Branch)
	var p struct {
		Reviewers []string        `json:"reviewers"`
		Note      string          `json:"note"`
		Checks    json.RawMessage `json:"checks"`
	}
	if json.Unmarshal(payload, &p) != nil || len(branch.Changes) == 0 || len(p.Reviewers) > BranchMaxReviewer || len(p.Note) > 4096 || len(p.Checks) > BranchMaxChecks {
		return branchInvalid("A proposal needs a branch with changes, at most 16 reviewers and bounded notes")
	}
	seen := map[string]bool{}
	for _, reviewer := range p.Reviewers {
		if reviewer == "" || seen[reviewer] || reviewer == branch.Created.By {
			return branchInvalid("Reviewers are distinct builders other than the branch author")
		}
		seen[reviewer] = true
	}
	reviewers := p.Reviewers
	if reviewers == nil {
		reviewers = []string{}
	}
	branch.Proposal = &BranchProposal{By: c.ID, At: at.UTC(), Note: p.Note, Reviewers: reviewers, Reviews: []BranchReview{}, Checks: p.Checks}
	return nil
}

func reviewBranch(c platform.Caller, record any, payload json.RawMessage, at time.Time) *kernel.Error {
	branch := record.(*Branch)
	var p struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if json.Unmarshal(payload, &p) != nil || branch.Proposal == nil || !slices.Contains([]string{"approve", "changes"}, p.Decision) || len(p.Note) > 4096 {
		return branchInvalid("A review approves or asks for changes")
	}
	if !slices.Contains(branch.Proposal.Reviewers, c.ID) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only a named reviewer may review this proposal")
	}
	if len(branch.Proposal.Reviews) >= BranchMaxReviews {
		return branchInvalid("The proposal holds at most 64 reviews")
	}
	branch.Proposal.Reviews = append(branch.Proposal.Reviews, BranchReview{By: c.ID, At: at.UTC(), Decision: p.Decision, Note: p.Note})
	return nil
}

func (b Branch) Approved() error {
	if b.Proposal == nil {
		return fmt.Errorf("the branch has no proposal")
	}
	if len(b.Proposal.Reviewers) == 0 {
		return nil
	}
	latest := map[string]string{}
	for _, review := range b.Proposal.Reviews {
		latest[review.By] = review.Decision
	}
	approved := false
	for _, reviewer := range b.Proposal.Reviewers {
		switch latest[reviewer] {
		case "changes":
			return fmt.Errorf("reviewer %s asked for changes", reviewer)
		case "approve":
			approved = true
		}
	}
	if !approved {
		return fmt.Errorf("no reviewer approved the proposal")
	}
	return nil
}

func mergeBranch(c platform.Caller, record any, payload json.RawMessage, at time.Time) *kernel.Error {
	branch := record.(*Branch)
	var p struct {
		Candidate string          `json:"candidate"`
		Applied   json.RawMessage `json:"applied"`
	}
	if json.Unmarshal(payload, &p) != nil || len(p.Applied) > BranchMaxChecks {
		return branchInvalid("A merge records its candidate and the resources it changed")
	}
	if err := branch.Approved(); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	branch.Merge = &BranchMerge{By: c.ID, At: at.UTC(), Candidate: p.Candidate, Applied: p.Applied}
	return nil
}
