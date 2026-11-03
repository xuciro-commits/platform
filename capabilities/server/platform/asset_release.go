package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

type ReleasePublication struct {
	Schema string          `json:"schema"`
	Image  json.RawMessage `json:"image"`
}

const AcceptedResultMaxBytes = 4 << 20

func CanonicalDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

type DecisionDefinition struct {
	Observation       *ObservationDecisionEvidence `json:"observation,omitempty"`
	Definition        AssetBinding                 `json:"definition"`
	Contract          SchemaVersion                `json:"contract"`
	Closure           string                       `json:"closure,omitempty"`
	DependenciesState string                       `json:"dependenciesState"`
	Release           string                       `json:"release,omitempty"`
	Dependencies      []AssetBinding               `json:"dependencies"`
	Applications      []AssetBinding               `json:"applications"`
}

type ObservationDecisionEvidence struct {
	Key        string                     `json:"key"`
	Definition AssetBinding               `json:"definition"`
	Record     string                     `json:"record"`
	Device     string                     `json:"device"`
	Signal     string                     `json:"signal"`
	Window     ObservationWindow          `json:"window"`
	Intervals  *ObservationIntervalResult `json:"intervals,omitempty"`
}

func (b DecisionDefinition) Check(app, schema string) error {
	if b.Observation != nil {
		if err := b.Observation.Check(); err != nil {
			return err
		}
	}
	if b.Definition.Ref != (AssetRef{App: app, Kind: AssetAction, Name: schema}) || b.Definition.SourceVersion == "" || b.Contract.Check() != nil || len(b.Dependencies) > 1000 || len(b.Applications) > 1000 || slices.ContainsFunc(b.Applications, func(binding AssetBinding) bool { return binding.Ref.Kind != AssetApp }) {
		return fmt.Errorf("decision definition differs from its receipt")
	}
	for _, bindings := range [][]AssetBinding{b.Dependencies, b.Applications} {
		seen := map[AssetRef]bool{}
		for _, binding := range bindings {
			if binding.Ref.Check() != nil || binding.SourceVersion == "" || seen[binding.Ref] {
				return fmt.Errorf("decision dependency has an invalid or duplicate binding")
			}
			seen[binding.Ref] = true
		}
	}
	return nil
}

func (e ObservationDecisionEvidence) Check() error {
	if !strings.HasPrefix(e.Key, "observation:") || len(e.Key) > 200 || e.Definition.Ref.Kind != AssetObservation || e.Definition.Ref.Check() != nil || e.Definition.SourceVersion == "" || e.Record == "" || e.Device == "" || e.Signal == "" {
		return fmt.Errorf("observation decision evidence needs its original source and record")
	}
	if e.Intervals != nil {
		if e.Window != (ObservationWindow{}) {
			return fmt.Errorf("state observation evidence cannot contain a numeric window")
		}
		return e.Intervals.Check()
	}
	return e.Window.Check()
}

type ReleaseOrigin struct {
	From   string    `json:"from"`
	Member string    `json:"member"`
	At     time.Time `json:"at"`
}

type SealedArtifact struct {
	Tenant    string    `json:"tenant"`
	Candidate string    `json:"candidate"`
	Key       string    `json:"key"`
	Digest    string    `json:"digest"`
	Size      int       `json:"size"`
	Assets    int       `json:"assets"`
	SealedAt  time.Time `json:"sealedAt"`
}

const EmptyRecordHistory = "history.v1.empty"

func AppendRecordHistory(prior string, position int, change json.RawMessage) (string, error) {
	if position < 0 || position == 0 && prior != EmptyRecordHistory {
		return "", fmt.Errorf("invalid history chain predecessor")
	}
	if position > 0 {
		raw, err := hex.DecodeString(prior)
		if err != nil || len(raw) != sha256.Size {
			return "", fmt.Errorf("invalid history chain digest")
		}
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(change))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	raw, err := json.Marshal([]any{"platform.record-history.v1", position, prior, value})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type ReleaseInstallation struct {
	ReleasePublication
	Before string    `json:"before"`
	Ref    *AssetRef `json:"ref,omitempty"`
	Record string    `json:"record,omitempty"`
}

func DecodeJSON(raw []byte, target any, name string) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%s has trailing data", name)
	}
	return nil
}
