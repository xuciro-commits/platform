package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
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
	if !Named(m.Name) {
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
	return definitionReleaseAsset(platform.AssetRef{App: ID, Kind: platform.AssetSampleSet, Name: m.Name}, ValueDefinitionVersion+".samples-"+strconv.Itoa(version), m.descriptor())
}

func decodeSampleAsset(asset platform.ReleaseAsset) (platform.SampleSet, int, error) {
	var d platform.SampleSet
	ordinal, ok := strings.CutPrefix(asset.SourceVersion, ValueDefinitionVersion+".samples-")
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

func (*Build) testPlanEntity() platform.Entity {
	return platform.Entity{Type: TestPlanType, Title: "Test plan", Plural: "Test plans", Model: TestPlan{}, Display: "title",
		Description: "Fixed sample inputs and expected outcomes for testing a saved object, workflow or function draft.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "objects"}}
}

func checkTestPlanFields(raw []byte) *kernel.Error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if given, ok := fields["at"]; ok {
		var at time.Time
		if json.Unmarshal(given, &at) != nil || at.IsZero() {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a valid fixed test time")
		}
	}
	if given, ok := fields["evaluation"]; ok && !bytes.Equal(bytes.TrimSpace(given), []byte("null")) {
		var policy []platform.EvaluationPolicy
		decoder := json.NewDecoder(bytes.NewReader(given))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&policy) != nil || len(policy) != 1 || decoder.Decode(new(any)) != io.EOF {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Function evaluation needs quality, total USD cost and per-call latency thresholds")
		}
	}
	if given, ok := fields["steps"]; ok {
		var steps []platform.TestStep
		decoder := json.NewDecoder(bytes.NewReader(given))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&steps) != nil || len(steps) < 1 || len(steps) > 20 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A test plan needs 1–20 typed steps")
		}
		for _, step := range steps {
			if err := step.Check(); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
		}
	}
	return nil
}

func (b *Build) checkTestPlan(c platform.Caller, s *pb.Submission) *kernel.Error {
	if !c.Holds(c.App, Builder) {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if err := checkTestPlanFields(s.GetPayload()); err != nil {
		return err
	}
	var plan TestPlan
	var previous TestPlan
	if s.GetSchema().GetName() == TestPlanType+".edit" {
		plan, _ = platform.Get[TestPlan](c, s.GetTarget().GetId())
		previous = plan
	}
	if json.Unmarshal(s.GetPayload(), &plan) != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if previous.Published != "" && plan.Name != previous.Name {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published definition cannot be renamed; create another definition")
	}
	if previous.Published != "" && testPlanKind(previous) != testPlanKind(plan) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published test plan keeps its kind; create another plan")
	}
	if plan.Name != "" {
		rows, err := readDefinitionInventory[TestPlan](b.host.Automation(c, ID))
		if err != nil {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		for _, row := range rows {
			if row.ID != s.GetTarget().GetId() && row.Name == plan.Name {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This definition name is already in use")
			}
		}
	}
	roots := 0
	for _, root := range []string{string(plan.Object), string(plan.Process), string(plan.Function), string(plan.Application)} {
		if root != "" {
			roots++
		}
	}
	if err := platform.CheckSimulationOrganization(plan.Organization); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if len(plan.Roots) > 0 {
		if roots != 0 || plan.CandidateID != "" || len(plan.Actors) > 0 || plan.As != "" || plan.Sample != "" || plan.Model != "" || len(plan.Evaluation) > 0 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Portable test roots cannot include local draft, member or model bindings")
		}
		if err := (platform.TestPlanDescriptor{Name: plan.Name, Title: plan.Title, Roots: plan.Roots, At: plan.At, Organization: plan.Organization, Samples: plan.Samples, Steps: plan.Steps}).Check(); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		return nil
	}
	if (roots != 1 && !(roots == 0 && plan.CandidateID != "")) || plan.CandidateID != "" && (plan.Object != "" || plan.Process != "" || plan.Function != "") || len(plan.Actors) > 3 || len(plan.Sample) > 64 || len(plan.Actors) > 0 && plan.As != "" || len(plan.Model) > 256 || plan.FunctionVersion < 0 || plan.FunctionVersion > 64 || plan.Name != "" && !Named(plan.Name) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose exactly one saved object, workflow or function draft for the test plan")
	}
	if len(plan.Evaluation) != 0 {
		if len(plan.Evaluation) != 1 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Function evaluation needs quality, total USD cost and per-call latency thresholds")
		}
		if plan.Function == "" || plan.Evaluation[0].Check() != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Function evaluation needs quality, total USD cost and per-call latency thresholds")
		}
	}
	return nil
}

const TestPlanType = "build.testplan"

type TestPlan struct {
	platform.Record
	Organization      *platform.OrgSeed           `json:"organization,omitempty" type:"json" field:"aside" title:"Synthetic organization"`
	Roots             []platform.AssetRef         `json:"roots,omitempty" type:"json" title:"Portable definition roots"`
	Application       string                      `json:"application,omitempty" ref:"build.app" title:"Saved application draft"`
	CandidateID       string                      `json:"candidateId,omitempty" title:"Fixed candidate version"`
	Actors            []string                    `json:"actors,omitempty" title:"Comparison members"`
	Sample            string                      `json:"sample,omitempty" title:"Application sample set"`
	Name              string                      `json:"name,omitempty" title:"Evaluation set name"`
	FunctionVersion   int                         `json:"functionVersion,omitempty" title:"Evaluated function version"`
	DefinitionVersion int                         `json:"definitionVersion,omitempty" field:"readonly"`
	Published         string                      `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions          []string                    `json:"versions,omitempty" field:"readonly"`
	Title             string                      `json:"title" field:"required,search" title:"Test plan name"`
	Object            platform.Ref[Object]        `json:"object,omitempty" title:"Saved object draft"`
	Process           platform.Ref[Process]       `json:"process,omitempty" title:"Saved workflow draft"`
	Function          platform.Ref[Function]      `json:"function,omitempty" title:"Saved function draft"`
	Model             string                      `json:"model,omitempty" title:"Model identifier"`
	As                string                      `json:"as,omitempty" title:"Member ID (empty: you)"`
	At                time.Time                   `json:"at" field:"required" title:"Fixed test time"`
	Steps             []platform.TestStep         `json:"steps" field:"required,aside" title:"Test steps"`
	Samples           []platform.SampleRecords    `json:"samples,omitempty" field:"aside" type:"json"`
	Evaluation        []platform.EvaluationPolicy `json:"evaluation,omitempty" field:"aside" title:"Release evaluation thresholds"`
}
