package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const SampleSetType = "build.sample-set"
const SchemaSampleSet = SampleSetType + ".publish"

type SampleSetRecord struct {
	platform.Record
	Name              string                   `json:"name" field:"required,search"`
	Title             string                   `json:"title" field:"required,search"`
	Samples           []platform.SampleRecords `json:"samples" type:"json" field:"required"`
	State             string                   `json:"state" field:"readonly" choices:"draft,published"`
	DefinitionVersion int                      `json:"definitionVersion,omitempty" field:"readonly" title:"Definition version"`
	Published         string                   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string                 `json:"versions,omitempty" field:"readonly" title:"Published versions"`
}

func (m SampleSetRecord) descriptor() platform.SampleSet {
	return platform.SampleSet{Name: m.Name, Title: m.Title, Samples: m.Samples}
}

func (b *Build) sampleSetEntity() platform.Entity {
	return platform.Entity{Type: SampleSetType, Title: "Sample set", Plural: "Sample sets", Model: SampleSetRecord{}, Display: "title",
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "objects"},
		Validate: func(c platform.Caller, record any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			m := record.(*SampleSetRecord)
			if err := b.checkSampleSet(*m); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Retain these synthetic records as an immutable application sample asset", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "objects", Payload: []platform.Field{}, Do: func(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
				m := record.(*SampleSetRecord)
				next, err := b.retainSampleSet(*m)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				*m = next
				return nil
			}}}}}
}

func (b *Build) sampleSetInventory() ([]SampleSetRecord, error) {
	return readDefinitionInventory[SampleSetRecord](b.host.Automation(platform.Caller{}, ID))
}

func (b *Build) checkSampleSet(m SampleSetRecord) error {
	if !named(m.Name) {
		return fmt.Errorf("The sample set name must use lower-case letters and digits")
	}
	if err := m.descriptor().Check(); err != nil {
		return err
	}
	rows, err := b.sampleSetInventory()
	if err != nil {
		return err
	}
	for _, other := range rows {
		if other.ID != m.ID && other.Name == m.Name {
			return fmt.Errorf("The sample set name is already used")
		}
	}
	if m.Published != "" {
		var old platform.ReleaseAsset
		if json.Unmarshal([]byte(m.Published), &old) != nil || old.Ref.Name != m.Name {
			return fmt.Errorf("A published sample set keeps its name")
		}
	}
	return nil
}

func sampleSetAsset(m SampleSetRecord, version int) (platform.ReleaseAsset, error) {
	return definitionReleaseAsset(platform.AssetRef{App: ID, Kind: platform.AssetSampleSet, Name: m.Name}, definitionVersion+".samples-"+strconv.Itoa(version), m.descriptor())
}

func decodeSampleAsset(asset platform.ReleaseAsset) (platform.SampleSet, int, error) {
	var d platform.SampleSet
	ordinal, ok := strings.CutPrefix(asset.SourceVersion, definitionVersion+".samples-")
	version, err := strconv.Atoi(ordinal)
	if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetSampleSet || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &d) != nil || d.Name != asset.Ref.Name || d.Check() != nil || platform.CheckAssetDependencies(asset) != nil {
		return d, 0, fmt.Errorf("Invalid sample asset")
	}
	return d, version, nil
}

func (b *Build) retainSampleSet(m SampleSetRecord) (SampleSetRecord, error) {
	if err := b.checkSampleSet(m); err != nil {
		return m, err
	}
	if m.DefinitionVersion >= 64 {
		return m, fmt.Errorf("The sample set version family is full")
	}
	asset, err := sampleSetAsset(m, m.DefinitionVersion+1)
	if err != nil {
		return m, err
	}
	raw, err := json.Marshal(asset)
	if err != nil {
		return m, err
	}
	m.State, m.DefinitionVersion, m.Published = "published", m.DefinitionVersion+1, string(raw)
	m.Versions = append(slices.Clone(m.Versions), m.Published)
	return m, nil
}

func (b *Build) sampleSetAssets() ([]platform.ReleaseAsset, error) {
	return portableDefinitionAssets(b.sampleSetInventory, decodeSampleAsset, "sample set")
}

func (b *Build) sampleSetPinned(ref platform.AssetRef, version string) (platform.ReleaseAsset, error) {
	return pinnedDefinitionAsset(b.sampleSetInventory, decodeSampleAsset, ref, version, "sample set")
}

func (b *Build) sampleSetDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	rows, e := b.sampleSetInventory()
	if e != nil {
		err = e
		return
	}
	i := slices.IndexFunc(rows, func(m SampleSetRecord) bool { return m.ID == id && !m.Archived })
	if i < 0 {
		err = fmt.Errorf("Sample set draft not found")
		return
	}
	m := rows[i]
	if err = b.checkSampleSet(m); err != nil {
		return
	}
	if m.DefinitionVersion >= 64 {
		err = fmt.Errorf("The sample set version family is full")
		return
	}
	next = platform.AssetRef{App: ID, Kind: platform.AssetSampleSet, Name: m.Name}
	prior, hadPrior = next, m.Published != ""
	a, e := sampleSetAsset(m, m.DefinitionVersion+1)
	if e != nil {
		err = e
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(a platform.ReleaseAsset) bool { return a.Ref == next })
	after = append(after, a)
	return
}

func (b *Build) prepareSampleSetInstallations(assets []platform.ReleaseAsset) ([]platform.AssetInstallation, error) {
	rows, err := b.sampleSetInventory()
	if err != nil {
		return nil, err
	}
	out := []platform.AssetInstallation{}
	for _, a := range assets {
		if a.Ref.Kind != platform.AssetSampleSet || a.Ref.App != ID {
			continue
		}
		d, version, err := decodeSampleAsset(a)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(m SampleSetRecord) bool { return m.Name == d.Name && !m.Archived })
		if i < 0 {
			return nil, fmt.Errorf("Sample asset needs its owner draft")
		}
		m := rows[i]
		if version <= m.DefinitionVersion {
			prior, err := b.sampleSetPinned(a.Ref, a.SourceVersion)
			if err != nil || !equalSampleAssets(prior, a) {
				return nil, fmt.Errorf("Retained sample set bytes differ")
			}
			continue
		}
		if version != m.DefinitionVersion+1 {
			return nil, fmt.Errorf("Sample version must be the next retained version")
		}
		m.Title, m.Samples = d.Title, d.Samples
		m.State, m.DefinitionVersion = "published", version
		raw, _ := json.Marshal(a)
		m.Published = string(raw)
		m.Versions = append(slices.Clone(m.Versions), m.Published)
		image, _ := json.Marshal(m)
		out = append(out, platform.AssetInstallation{Ref: a.Ref, Schema: SchemaSampleSet, Record: SampleSetType + "/" + m.ID, Image: image})
	}
	return out, nil
}

func equalSampleAssets(a, b platform.ReleaseAsset) bool {
	var x, y any
	return a.Ref == b.Ref && a.SourceVersion == b.SourceVersion && a.ContractVersion == b.ContractVersion && slices.Equal(a.Requires, b.Requires) && json.Unmarshal(a.Body, &x) == nil && json.Unmarshal(b.Body, &y) == nil && reflect.DeepEqual(x, y)
}

func sampleSetInstallation(image json.RawMessage) (platform.AssetInstallation, error) {
	return portableDefinitionInstallation[SampleSetRecord](image, SampleSetType, SchemaSampleSet, "sample", decodeSampleAsset)
}
