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
