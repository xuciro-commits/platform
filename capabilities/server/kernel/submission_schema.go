package kernel

import (
	"encoding/json"
	"slices"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func (s *Sequence) PriorOf(a platform.Service, key string) Prior {
	return s.Committed.Prior(a.Manifest().ID, a.Authority(), s.ID, key)
}

func (s *Sequence) CurrentSchema(app platform.Service, sub *pb.Submission) *pb.Submission {
	if sub.GetSchema().GetVersion() != 1 {
		return sub
	}
	return s.PriorOf(app, sub.GetIdempotencyKey()).CurrentSchema(app, sub)
}

func (prior Prior) CurrentSchema(app platform.Service, sub *pb.Submission) *pb.Submission {
	if sub.GetSchema().GetVersion() != 1 {
		return sub
	}
	family := uint32(1)
	if len(prior.Answer) > 0 {
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

func (s *Sequence) SchemaBinding(app platform.Service, sub *pb.Submission, content string, schemaOf func(platform.Service, platform.Action) (platform.SchemaVersion, error)) (*platform.SchemaVersion, *platform.Error) {
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
		if action.SchemaVersion == nil && current.Family == 1 && sub.GetSchema().GetVersion() == 1 {
			return nil, nil
		}
		key := app.Manifest().ID + "/" + sub.GetIdempotencyKey()
		if _, bound := s.Committed.SchemaVersion(key); !bound && app.Authority().AcceptedFor(s.ID, sub.GetIdempotencyKey()) != nil {
			return nil, nil
		}
		if prior, exists := s.Committed.Refusal(key); exists && prior.SchemaVersion == nil {
			return nil, nil
		}
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The submission needs its schema content version; refresh definitions")
	}
	if binding.Version == current.Version && binding.Family == current.Family {
		return binding, nil
	}
	if et := s.Projection.Type("build.object"); et != nil && binding.Family == current.Family {
		matches := func(row *records.Row) bool {
			var object struct {
				Name           string                   `json:"name"`
				Namespace      string                   `json:"namespace"`
				SchemaVersions []platform.SchemaVersion `json:"schemaVersions"`
			}
			object.Namespace = "build"
			published := records.BusinessField(et.Info(), row.Value, "published")
			return json.Unmarshal([]byte(published), &object) == nil && object.Namespace+"."+object.Name == action.Target && slices.Contains(object.SchemaVersions, *binding)
		}
		if directory := s.Projection.Type("build.resource"); directory != nil {
			ref := platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: action.Target}
			if entry := directory.Rows().Get(protocol.ResourceID(s.ID, ref)); entry != nil {
				resource := entry.Value.Interface().(protocol.Resource)
				if row := et.Rows().Get(resource.Draft); row != nil && matches(row) {
					return binding, nil
				}
			}
		}
		for _, row := range et.Rows().All() {
			if matches(row) {
				return binding, nil
			}
		}
	}
	return binding, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The schema version is unknown or belongs to another family; refresh definitions")
}

func (s *Sequence) ReplayKnownSubmission(app platform.Service, sub *pb.Submission) (*pb.ChangeRecord, *platform.Error, bool) {
	if app == nil {
		return nil, nil, false
	}
	prior := s.PriorOf(app, sub.GetIdempotencyKey())
	if !prior.Known() {
		return nil, nil, false
	}
	record, _, refusal, done := s.ReplayPrior(app, prior.CurrentSchema(app, sub), nil, prior)
	return record, refusal, done
}
