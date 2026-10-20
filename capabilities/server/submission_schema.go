package platformserver

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const SchemaVersionHeader = "Platform-Schema-Version"

func (t *Tenant) currentSubmissionSchema(app platform.App, sub *pb.Submission) *pb.Submission {
	if sub.GetSchema().GetVersion() != 1 {
		return sub
	}
	family := uint32(1)
	key := app.Manifest().ID + "/" + sub.GetIdempotencyKey()
	if raw := t.committed.answer(key); len(raw) > 0 {
		result, receipt, err := decodeAcceptedBatch(raw)
		if err == nil {
			saved, err := batchSubmission(result, receipt)
			if err == nil {
				family = saved.GetSchema().GetVersion()
			}
		}
	} else if prior := app.AcceptedLedger().AcceptedFor(t.ID, sub.GetIdempotencyKey()); prior != nil {
		family = prior.GetSubmission().GetSchema().GetVersion()
	} else if prior, exists := t.committed.refusal(key); exists {
		_, saved, err := decodeRefusedResult(platform.Raw(prior))
		if err == nil {
			family = saved.GetSchema().GetVersion()
		}
	} else if action, exists := app.Manifest().Actions.Action(sub.GetSchema().GetName()); exists && action.SchemaVersion != nil {
		family = action.SchemaVersion.Family
	}
	if family == 1 {
		return sub
	}
	copy := proto.Clone(sub).(*pb.Submission)
	copy.Schema.Version = family
	return copy
}

func validateSubmissionSchemaVersion(sub *pb.Submission, binding *platform.SchemaVersion) error {
	if binding == nil {
		return nil
	}
	if err := binding.Check(); err != nil {
		return err
	}
	if sub.GetSchema().GetVersion() != binding.Family {
		return fmt.Errorf("submission family differs from its saved schema binding")
	}
	return nil
}

func (t *Tenant) actionSchemaVersion(app platform.App, action platform.Action) (platform.SchemaVersion, error) {
	if action.SchemaVersion != nil {
		return *action.SchemaVersion, nil
	}
	manifest := app.Manifest()
	contract := struct {
		App, Version   string
		Action, Object any
	}{App: manifest.ID, Version: manifest.Version, Action: codeActionDescriptor(action)}
	for _, definition := range t.definitions {
		if definition.Source != "code" || definition.Ref.App != manifest.ID || definition.Ref.Kind != platform.AssetObject || definition.Ref.Name != action.Target || definition.Entity == nil {
			continue
		}
		for _, entity := range manifest.Entities {
			if entity.Type == action.Target {
				contract.Object = codeObjectDescriptor(entity, *definition.Entity)
				break
			}
		}
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		return platform.SchemaVersion{}, err
	}
	version, err := platform.SchemaContentVersion(raw)
	return platform.SchemaVersion{Version: version, Family: 1}, err
}

func (t *Tenant) submissionSchemaVersion(app platform.App, sub *pb.Submission, content string) (*platform.SchemaVersion, *kernel.Error) {
	var binding *platform.SchemaVersion
	if content != "" {
		binding = &platform.SchemaVersion{Version: content, Family: sub.GetSchema().GetVersion()}
		if binding.Check() != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The submission schema content version is invalid")
		}
	}
	action, exists := app.Manifest().Actions.Action(sub.GetSchema().GetName())
	if !exists {
		return binding, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The action definition is unavailable; refresh definitions")
	}
	current, err := t.actionSchemaVersion(app, action)
	if err != nil {
		return binding, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The action definition is unavailable; refresh definitions")
	}
	if content == "" {
		key := app.Manifest().ID + "/" + sub.GetIdempotencyKey()
		if _, bound := t.committed.schemaVersion(key); !bound && app.AcceptedLedger().AcceptedFor(t.ID, sub.GetIdempotencyKey()) != nil {
			return nil, nil // Retry an original unbound receipt without upgrading its bytes.
		}
		if prior, exists := t.committed.refusal(key); exists && prior.SchemaVersion == nil {
			return nil, nil
		}
		if action.SchemaVersion == nil && current.Family == 1 && sub.GetSchema().GetVersion() == 1 {
			// Existing native package clients retain their original v1 contract.
			// Published builder objects always require their content declaration.
			return nil, nil
		}
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The submission needs its schema content version; refresh definitions")
	}
	if binding.Version == current.Version && binding.Family == current.Family {
		return binding, nil
	}
	if et := t.records.types["build.object"]; et != nil {
		for _, row := range et.rows.All() {
			var metadata struct {
				Published string `json:"published"`
			}
			raw, _ := row.Image()
			if json.Unmarshal(raw, &metadata) != nil {
				continue
			}
			var object struct {
				Name           string                   `json:"name"`
				SchemaVersions []platform.SchemaVersion `json:"schemaVersions"`
			}
			if json.Unmarshal([]byte(metadata.Published), &object) != nil || "build."+object.Name != action.Target {
				continue
			}
			if slices.Contains(object.SchemaVersions, *binding) && binding.Family == current.Family {
				return binding, nil
			}
		}
	}
	return binding, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The schema version is unknown or belongs to another family; refresh definitions")
}

// The HTTP edge supplies the exact declaration it showed the client. Internal
// Go callers use the current owner catalog through Submit; old Flow/approval
// execution keeps the catalog bound to its retained release.
func (t *Tenant) submitSchemaDiagnosed(member platform.Member, sub *pb.Submission, content string, at time.Time) (*pb.ChangeRecord, *kernel.Error, []platform.FieldIssue) {
	var issues []platform.FieldIssue
	record, refusal := t.submit(member, sub, at, func(_ *kernel.Error, found []platform.FieldIssue) { issues = found }, strings.TrimSpace(content))
	return record, refusal, t.readableFieldIssues(member, issues, at)
}
