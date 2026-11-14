package kernel

import (
	"encoding/json"
	"slices"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

func (s *Sequence) PriorOf(a platform.App, key string) Prior {
	return s.Committed.Prior(a.Manifest().ID, a.AcceptedLedger(), s.ID, key)
}

func (s *Sequence) CurrentSchema(app platform.App, sub *pb.Submission) *pb.Submission {
	if sub.GetSchema().GetVersion() != 1 {
		return sub
	}
	family := uint32(1)
	if prior := s.PriorOf(app, sub.GetIdempotencyKey()); len(prior.Answer) > 0 {
		result, receipt, err := acceptedformat.DecodeBatch(prior.Answer)
		if err == nil {
			saved, err := acceptedformat.BatchSubmission(result, receipt)
			if err == nil {
				family = saved.GetSchema().GetVersion()
			}
		}
	} else if prior.Record != nil {
		family = prior.Record.GetSubmission().GetSchema().GetVersion()
	} else if prior.Refusal != nil {
		_, saved, err := acceptedformat.DecodeRefusal(platform.Raw(*prior.Refusal))
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

func (s *Sequence) SchemaBinding(app platform.App, sub *pb.Submission, content string, schemaOf func(platform.App, platform.Action) (platform.SchemaVersion, error)) (*platform.SchemaVersion, *kernel.Error) {
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
	current, err := schemaOf(app, action)
	if err != nil {
		return binding, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The action definition is unavailable; refresh definitions")
	}
	if content == "" {
		key := app.Manifest().ID + "/" + sub.GetIdempotencyKey()
		if _, bound := s.Committed.SchemaVersion(key); !bound && app.AcceptedLedger().AcceptedFor(s.ID, sub.GetIdempotencyKey()) != nil {
			return nil, nil
		}
		if prior, exists := s.Committed.Refusal(key); exists && prior.SchemaVersion == nil {
			return nil, nil
		}
		if action.SchemaVersion == nil && current.Family == 1 && sub.GetSchema().GetVersion() == 1 {
			return nil, nil
		}
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The submission needs its schema content version; refresh definitions")
	}
	if binding.Version == current.Version && binding.Family == current.Family {
		return binding, nil
	}
	if et := s.Projection.Type("build.object"); et != nil {
		for _, row := range et.Rows().All() {
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
