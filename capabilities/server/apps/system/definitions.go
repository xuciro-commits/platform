package system

import (
	"encoding/json"
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (s *System) AttachSystemDefinitions(h platform.SystemDefinitionHost) { s.definitionHost = h }

func (s *System) freezeDefinitions(c platform.Caller, change *Change, now time.Time) *kernel.Error {
	if s.definitionHost == nil {
		if len(change.Assets) == 0 {
			return nil
		}
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The system definition owner is unavailable")
	}
	candidate, refusal := s.definitionHost.FreezeSystemDefinitions(c, change.Assets, now)
	if refusal != nil {
		return refusal
	}
	change.Candidate = candidate
	return nil
}

// An approved definition-only change needs no process build. It still gets
// the same immutable release and explicit publisher activation as source work.
func (s *System) approveDefinitions(c platform.Caller, change *Change, now time.Time) (*Release, *kernel.Error) {
	if len(change.Files) != 0 || len(change.Candidate) == 0 {
		return nil, nil
	}
	releases, _, refusal := platform.Find[Release](c, platform.Query{Sort: []string{"-version"}, Limit: 1})
	if refusal != nil {
		return nil, refusal
	}
	previous, version := change.Base, 1
	if len(releases) > 0 {
		version = releases[0].Version + 1
	}
	release := &Release{Record: platform.Record{ID: fmt.Sprintf("%04d", version)}, Version: version, Change: change.ID,
		Manifest: change.Manifest, Previous: previous, SourceDigest: change.SourceDigest,
		Candidate: change.Candidate, Kind: "definitions", State: StatePublished}
	change.State, change.Release = StatePublished, version
	return release, nil
}

func (s *System) prepareDefinitionActivation(c platform.Caller, activation *Activation, release *Release, now time.Time) *kernel.Error {
	if len(release.Candidate) == 0 {
		return nil
	}
	// Code changes install only when the updater reports a successful swap.
	if release.Kind != "definitions" && activation.Result != StateActive {
		return nil
	}
	if s.definitionHost == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The system definition owner is unavailable")
	}
	image, refusal := s.definitionHost.PrepareSystemDefinitions(c, release.Candidate, "system:"+activation.ID, now)
	if refusal != nil {
		return refusal
	}
	activation.Installation = image
	if release.Kind == "definitions" {
		release.Installation = image
		activation.Result, release.State = StateActive, StateActive
	}
	return nil
}

func (*System) AcceptedPublicationSchemas() []string {
	return []string{SchemaActivate, SchemaFinished, SchemaDeactivate}
}

func systemDefinitionImage(schema string, image []byte) (json.RawMessage, error) {
	switch schema {
	case SchemaActivate, SchemaDeactivate:
		var release Release
		if err := json.Unmarshal(image, &release); err != nil {
			return nil, err
		}
		if schema == SchemaActivate && len(release.Installation) > 0 && (release.Kind != "definitions" || release.State != StateActive) {
			return nil, fmt.Errorf("definition installation has no active definition release")
		}
		return release.Installation, nil
	case SchemaFinished:
		var activation Activation
		if err := json.Unmarshal(image, &activation); err != nil {
			return nil, err
		}
		if len(activation.Installation) > 0 && activation.Result != StateActive {
			return nil, fmt.Errorf("definition installation has no successful activation")
		}
		return activation.Installation, nil
	}
	return nil, fmt.Errorf("unknown system publication %s", schema)
}

func (s *System) ValidateAcceptedPublication(schema string, image []byte) error {
	raw, err := systemDefinitionImage(schema, image)
	if err != nil || len(raw) == 0 {
		return err
	}
	if s.definitionHost == nil {
		return fmt.Errorf("system definition owner is unavailable")
	}
	return s.definitionHost.ValidateSystemDefinitions(raw)
}
func (s *System) ApplyAcceptedPublication(schema string, image []byte) error {
	raw, err := systemDefinitionImage(schema, image)
	if err != nil || len(raw) == 0 {
		return err
	}
	return s.definitionHost.ApplySystemDefinitions(raw)
}

var _ platform.AcceptedPublisher = (*System)(nil)

// The rollback action targets the current release, so its accepted publication
// must carry the target version's freshly prepared immutable installation.
func (s *System) prepareDefinitionRollback(c platform.Caller, activation *Activation, current, target *Release, now time.Time) *kernel.Error {
	if s.definitionHost == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The system definition owner is unavailable")
	}
	if len(target.Candidate) == 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The previous release has no frozen definition baseline; it cannot be restored safely")
	}
	image, refusal := s.definitionHost.PrepareSystemDefinitions(c, target.Candidate, "system:"+activation.ID, now)
	if refusal != nil {
		return refusal
	}
	activation.Installation, current.Installation = image, image
	activation.Result, target.State = StateActive, StateActive
	current.State = StateSuperseded
	return nil
}
