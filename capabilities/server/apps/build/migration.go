package build

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const migrationStart = platform.MigrationRunType + ".start"
const migrationAdvance = platform.MigrationRunType + ".advance"
const migrationCancel = platform.MigrationRunType + ".cancel"

func migrationEntity() platform.Entity {
	return platform.Entity{Type: platform.MigrationRunType, Title: "Migration", Plural: "Migrations", Model: platform.MigrationRun{}, Display: "review"}
}

func migrationActions() []platform.Action {
	return []platform.Action{
		{Schema: migrationStart, Target: platform.MigrationRunType, Capability: "migrations", Title: "Start migration", Description: "Start the reviewed migration between sealed releases", Roles: []string{construction.Builder}, New: true, Payload: []platform.Field{{Name: "plan", Type: "json", Description: "The reviewed migration plan"}, {Name: "asset", Type: "json", Description: "The pinned migration plan asset"}, {Name: "review", Type: "string", Required: true, Description: "The sealed review identity"}, {Name: "reviewedAt", Type: "datetime", Required: true, Description: "The captured migration review time"}}},
		{Schema: migrationAdvance, Target: platform.MigrationRunType, Capability: "migrations", Title: "Run migration batch", Description: "Accept the next bounded migration batch and its progress", Roles: []string{construction.Builder}, Payload: []platform.Field{}},
		{Schema: migrationCancel, Target: platform.MigrationRunType, Capability: "migrations", Title: "Cancel migration", Description: "Restore source facts in bounded accepted batches before releasing the migration", Roles: []string{construction.Builder}, Payload: []platform.Field{}},
	}
}

func (b *Build) submitMigration(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var input struct {
			Plan       platform.MigrationPlan `json:"plan"`
			Review     string                 `json:"review"`
			ReviewedAt time.Time              `json:"reviewedAt"`
			Asset      *platform.AssetBinding `json:"asset,omitempty"`
		}
		var run platform.MigrationRun
		if s.GetSchema().GetName() == migrationStart {
			decoder := json.NewDecoder(bytes.NewReader(s.GetPayload()))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&input); err != nil || decoder.Decode(new(any)) != io.EOF || input.Review == "" || input.ReviewedAt.IsZero() || input.ReviewedAt.After(at) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A reviewed migration plan is required")
			}
			if _, exists := platform.Get[platform.MigrationRun](c, s.GetTarget().GetId()); exists {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The migration identity is already used")
			}
		} else {
			var found bool
			run, found = platform.Get[platform.MigrationRun](c, s.GetTarget().GetId())
			if !found {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Migration not found")
			}
			if s.GetSchema().GetName() == migrationCancel && run.State != "cancelling" && run.State != "cancelled" {
				run.State, run.Cursor = "cancelling", ""
			}
		}
		return func(r *pb.ChangeRecord) {
			var result platform.MigrationRun
			var err *kernel.Error
			if s.GetSchema().GetName() == migrationStart {
				plan, asset, refusal := b.freezeMigrationInput(c, r, input.Plan, input.Asset)
				if refusal != nil {
					c.FailDecision(refusal)
					return
				}
				result, err = c.BeginMigration(r, plan, input.Review, input.ReviewedAt, at)
				result.Asset = asset
			} else {
				result, err = c.AdvanceMigration(r, run, at)
			}
			if err != nil {
				c.FailDecision(err)
				return
			}
			if err := c.Put(r, result); err != nil {
				c.FailDecision(err)
			}
		}, nil
	})
}
