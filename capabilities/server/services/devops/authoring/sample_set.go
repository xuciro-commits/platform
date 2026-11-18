package authoring

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/devops/descriptor"
	"platformserver/services/resources/publication"
)

type sampleSetOwner struct {
	env descriptor.SampleSetEnvironment
}

func SampleSetPublication(env descriptor.SampleSetEnvironment) descriptor.SampleSetPublication {
	o := &sampleSetOwner{env: env}
	return descriptor.SampleSetPublication{Entity: o.entity, Asset: o.asset, Draft: o.draft, Assets: o.assets, Resolve: o.resolve,
		PrepareInstallations: o.prepare, DecodeInstallation: o.decodeInstallation, ReadFrozen: o.readFrozen, VersionedDraft: o.draftAssets}
}

func (o *sampleSetOwner) entity() platform.RecordType {
	return platform.RecordType{Type: o.env.Type, Title: "Sample set", Plural: "Sample sets", Model: descriptor.SampleSetRecord{}, Display: "title",
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{o.env.Builder: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{o.env.Builder}, Capability: "objects"},
		Validate: func(c platform.Caller, record any) *platform.Error {
			if c.Replaying {
				return nil
			}
			m := record.(*descriptor.SampleSetRecord)
			if err := o.check(*m); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Actions: []platform.ActionType{{
				Name:        "publish",
				Title:       "Publish",
				Description: "Retain these synthetic records as an immutable application sample asset",
				Roles:       []string{o.env.Builder},
				Capability:  "objects",
				Parameters:  []platform.Field{},
				Criteria:    platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"draft", "published"}}},
				Rules: []platform.ActionRule{{Function: func(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *platform.Error {
					m := record.(*descriptor.SampleSetRecord)
					next, err := o.retain(*m)
					if err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
					}
					*m = next
					return nil
				}}, {Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"published"}}}},
			}}}}
}

func (o *sampleSetOwner) inventory() ([]descriptor.SampleSetRecord, error) {
	return publication.Inventory[descriptor.SampleSetRecord](o.env.Caller())
}

func (o *sampleSetOwner) check(m descriptor.SampleSetRecord) error {
	if !publication.ValidName(m.Name) {
		return fmt.Errorf("The sample set name must use lower-case letters and digits")
	}
	if err := m.Descriptor().Check(); err != nil {
		return err
	}
	rows, err := o.inventory()
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

func (o *sampleSetOwner) asset(m descriptor.SampleSetRecord, version int) (platform.ReleaseAsset, error) {
	body, err := json.Marshal(m.Descriptor())
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return o.env.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: o.env.App, Kind: platform.AssetSampleSet, Name: m.Name}, ContractVersion: 1, SourceVersion: o.env.Version + ".samples-" + strconv.Itoa(version), Body: body})
}

func (o *sampleSetOwner) draft(asset platform.ReleaseAsset) (descriptor.SampleSetRecord, error) {
	d, _, err := o.decode(asset)
	return descriptor.SampleSetRecord{Name: d.Name, Title: d.Title, Samples: d.Samples}, err
}

func (o *sampleSetOwner) assets() ([]platform.ReleaseAsset, error) {
	rows, err := o.inventory()
	if err != nil {
		return nil, err
	}
	out := []platform.ReleaseAsset{}
	for _, record := range rows {
		if record.Archived || record.Published == "" {
			continue
		}
		var asset platform.ReleaseAsset
		if json.Unmarshal([]byte(record.Published), &asset) != nil {
			return nil, fmt.Errorf("Invalid retained sample set")
		}
		if _, _, err := o.decode(asset); err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func (o *sampleSetOwner) resolve(ref platform.AssetRef, version string) (platform.ReleaseAsset, error) {
	rows, err := o.inventory()
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, record := range rows {
		for _, raw := range record.Versions {
			var asset platform.ReleaseAsset
			if json.Unmarshal([]byte(raw), &asset) == nil && asset.Ref == ref && asset.SourceVersion == version {
				_, _, err := o.decode(asset)
				return asset, err
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("The sample set version is not retained")
}

func (o *sampleSetOwner) decodeInstallation(image json.RawMessage) (protocol.AssetInstallation, error) {
	var record descriptor.SampleSetRecord
	var asset platform.ReleaseAsset
	if json.Unmarshal(image, &record) != nil || record.ID == "" || record.State != "published" || json.Unmarshal([]byte(record.Published), &asset) != nil {
		return protocol.AssetInstallation{}, fmt.Errorf("Invalid sample installation")
	}
	_, version, err := o.decode(asset)
	if err != nil || version != record.DefinitionVersion || asset.Ref.Name != record.Name || len(record.Versions) != version || record.Versions[len(record.Versions)-1] != record.Published {
		return protocol.AssetInstallation{}, fmt.Errorf("Invalid sample version history")
	}
	for i, raw := range record.Versions {
		var prior platform.ReleaseAsset
		if json.Unmarshal([]byte(raw), &prior) != nil {
			return protocol.AssetInstallation{}, fmt.Errorf("Invalid sample version history")
		}
		if _, ordinal, err := o.decode(prior); err != nil || ordinal != i+1 || prior.Ref.Name != record.Name {
			return protocol.AssetInstallation{}, fmt.Errorf("Invalid sample version history")
		}
	}
	return protocol.AssetInstallation{Ref: asset.Ref, Schema: o.env.Type + ".publish", Record: o.env.Type + "/" + record.ID, Image: slices.Clone(image)}, nil
}

func (*sampleSetOwner) readFrozen(image json.RawMessage) (json.RawMessage, error) {
	var record struct{ Published string }
	if err := json.Unmarshal(image, &record); err != nil {
		return nil, err
	}
	return json.RawMessage(record.Published), nil
}

func (o *sampleSetOwner) decode(asset platform.ReleaseAsset) (platform.SampleSet, int, error) {
	var d platform.SampleSet
	ordinal, ok := strings.CutPrefix(asset.SourceVersion, o.env.Version+".samples-")
	version, err := strconv.Atoi(ordinal)
	if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal || asset.Ref.App != o.env.App || asset.Ref.Kind != platform.AssetSampleSet || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &d) != nil || d.Name != asset.Ref.Name || d.Check() != nil || o.env.CheckDependencies(asset) != nil {
		return d, 0, fmt.Errorf("Invalid sample asset")
	}
	return d, version, nil
}

func (o *sampleSetOwner) retain(m descriptor.SampleSetRecord) (descriptor.SampleSetRecord, error) {
	if err := o.check(m); err != nil {
		return m, err
	}
	if m.DefinitionVersion >= 64 {
		return m, fmt.Errorf("The sample set version family is full")
	}
	asset, err := o.asset(m, m.DefinitionVersion+1)
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

func (o *sampleSetOwner) draftAssets(id string, releaseAssets func() ([]platform.ReleaseAsset, error)) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = releaseAssets()
	if err != nil {
		return
	}
	rows, e := o.inventory()
	if e != nil {
		err = e
		return
	}
	i := slices.IndexFunc(rows, func(m descriptor.SampleSetRecord) bool { return m.ID == id && !m.Archived })
	if i < 0 {
		err = fmt.Errorf("Sample set draft not found")
		return
	}
	m := rows[i]
	if err = o.check(m); err != nil {
		return
	}
	if m.DefinitionVersion >= 64 {
		err = fmt.Errorf("The sample set version family is full")
		return
	}
	next = platform.AssetRef{App: o.env.App, Kind: platform.AssetSampleSet, Name: m.Name}
	prior, hadPrior = next, m.Published != ""
	a, e := o.asset(m, m.DefinitionVersion+1)
	if e != nil {
		err = e
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(a platform.ReleaseAsset) bool { return a.Ref == next })
	after = append(after, a)
	return
}

func (o *sampleSetOwner) prepare(assets []platform.ReleaseAsset) ([]protocol.AssetInstallation, error) {
	rows, err := o.inventory()
	if err != nil {
		return nil, err
	}
	out := []protocol.AssetInstallation{}

	for _, a := range assets {
		if a.Ref.Kind != platform.AssetSampleSet || a.Ref.App != o.env.App {
			continue
		}
		d, version, err := o.decode(a)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(m descriptor.SampleSetRecord) bool { return m.Name == d.Name && !m.Archived })
		if i < 0 {
			return nil, fmt.Errorf("Sample asset needs its owner draft")
		}
		m := rows[i]
		if version <= m.DefinitionVersion {
			prior, err := o.resolve(a.Ref, a.SourceVersion)
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
		out = append(out, protocol.AssetInstallation{Ref: a.Ref, Schema: o.env.Type + ".publish", Record: o.env.Type + "/" + m.ID, Image: image})
	}
	return out, nil
}

func equalSampleAssets(a, b platform.ReleaseAsset) bool {
	var x, y any
	return a.Ref == b.Ref && a.SourceVersion == b.SourceVersion && a.ContractVersion == b.ContractVersion && slices.Equal(a.Requires, b.Requires) && json.Unmarshal(a.Body, &x) == nil && json.Unmarshal(b.Body, &y) == nil && reflect.DeepEqual(x, y)
}
