package data

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func MediaSetEntities() []platform.Entity {
	return []platform.Entity{
		{Type: platform.MediaSetType, Title: "Media set", Plural: "Media sets", Model: platform.MediaSet{}, Display: "title",
			Description: "A versioned collection of uploaded documents and images.",
			Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{"builder": platform.ScopeTenant, "integrator": platform.ScopeTenant}},
			Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{"builder", "integrator"}, Capability: "integrations"}},
		{Type: platform.MediaSetVersionType, Title: "Media set version", Plural: "Media set versions", Model: platform.MediaSetVersion{}, Display: "version",
			Description: "An immutable item snapshot retained for a media set.",
			Scope: platform.Scope{Through: func(record any) string {
				return platform.MediaSetType + "/" + record.(platform.MediaSetVersion).MediaSet
			}},
			Standard: platform.Standard{Roles: []string{"builder", "integrator"}, Capability: "integrations"}},
	}
}

func MediaSetActions() []platform.Action {
	return []platform.Action{
		platform.Action{Schema: platform.SchemaMediaSetAdd, Target: platform.MediaSetType, Title: "Add media item", Description: "Add an uploaded item as the next retained media set version.", Capability: "integrations", Roles: []string{"builder", "integrator"},
			Payload: []platform.Field{{Name: "hash", Type: "string", Required: true}, {Name: "name", Type: "string", Required: true}, {Name: "contentType", Type: "string", Required: true}, {Name: "size", Type: "integer", Required: true}}},
		platform.Action{Schema: platform.SchemaMediaSetRemove, Target: platform.MediaSetType, Title: "Remove media item", Description: "Remove an item from the latest version while retaining its earlier snapshots.", Capability: "integrations", Roles: []string{"builder", "integrator"},
			Payload: []platform.Field{{Name: "hash", Type: "string", Required: true}}},
	}
}

func SubmitMediaSet(ledger *platform.Ledger, files platform.FileHost, c platform.Caller, submission *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return ledger.Receive(c, submission, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		var input struct {
			Hash        string `json:"hash"`
			Name        string `json:"name"`
			ContentType string `json:"contentType"`
			Size        *int   `json:"size"`
		}
		set, ok := platform.Get[platform.MediaSet](c, submission.GetTarget().GetId())
		if !ok || set.Archived {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if json.Unmarshal(submission.GetPayload(), &input) != nil || !platform.ValidMediaHash(input.Hash) {
			return nil, invalid
		}
		index := slices.IndexFunc(set.Items, func(item platform.MediaItem) bool { return item.Hash == input.Hash })
		switch submission.GetSchema().GetName() {
		case platform.SchemaMediaSetAdd:
			if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.ContentType) == "" || input.Size == nil || *input.Size < 0 {
				return nil, invalid
			}
			if index >= 0 {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			if !c.Replaying && (files == nil || !files.Stored(c.Tenant, input.Hash)) {
				return nil, invalid
			}
			set.Items = append(slices.Clone(set.Items), platform.MediaItem{Hash: input.Hash, Name: strings.TrimSpace(input.Name), ContentType: input.ContentType, Size: *input.Size, By: c.ID, At: now})
		case platform.SchemaMediaSetRemove:
			if index < 0 {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			set.Items = slices.Delete(slices.Clone(set.Items), index, index+1)
		default:
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
		}
		set.Version++
		if err := c.Check(set); err != nil {
			return nil, err
		}
		version := platform.MediaSetVersion{Record: platform.Record{ID: platform.MediaSetVersionID(set.ID, set.Version)}, MediaSet: set.ID, Version: set.Version, Items: slices.Clone(set.Items), By: c.ID, At: now}
		if !c.Replaying {
			version.Request = submission.GetIdempotencyKey()
		}
		return func(record *pb.ChangeRecord) {
			c.Put(record, set)
			c.Put(record, version)
		}, nil
	})
}
