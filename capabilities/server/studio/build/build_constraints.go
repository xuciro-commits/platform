package build

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

const SchemaAlertPublish = AlertType + ".publish"

func (b *Build) alertEntity() platform.Entity {
	return platform.Entity{Type: AlertType, Title: "Alert rule", Plural: "Alert rules", Model: Alert{}, Display: "title",
		Description: "Tells a role when a record of an object comes to match a condition.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}},
		Validate: func(c platform.Caller, record any) *kernel.Error {
			a := record.(*Alert)
			if c.Replaying {
				return nil
			}
			if previous, ok := platform.Get[Alert](c, a.ID); ok {
				if legacyAlert(previous) {
					if err := freezeLegacyAlert(&previous); err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
					}
					a.State, a.Version, a.DefinitionVersion, a.Versions, a.Published = previous.State, previous.Version, previous.DefinitionVersion, previous.Versions, previous.Published
				}
				if previous.Published != "" && previous.Name != a.Name {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A published alert keeps its name")
				}
			}
			if err := b.checkAlert(*a); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			return nil
		},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Payload: []platform.Field{}, Do: b.publishAlert}}}}
}

func (b *Build) checkAlert(a Alert) error {
	if !Named(a.Name) {
		return fmt.Errorf("The alert name %s must be lower-case letters and digits", a.Name)
	}
	info, ok := b.lookupEntity(a.Object)
	if !ok {
		objects, err := readDefinitionInventory[Object](b.host.Automation(platform.Caller{}, ID))
		if err != nil {
			return err
		}
		for _, object := range objects {
			if !object.Archived && "build."+object.Name == a.Object {
				var err error
				info, err = platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
				ok = err == nil
				break
			}
		}
	}
	if !ok {
		return fmt.Errorf("%s is not a published object of this builder", a.Object)
	}
	if _, ok := info.Field(a.Field); !ok {
		return fmt.Errorf("%s has no field %s", a.Object, a.Field)
	}
	if !slices.Contains(Operators, a.Operator) {
		return fmt.Errorf("%q is not an operator", a.Operator)
	}
	if a.Role != "" && !slices.Contains(b.Manifest().AllRoles(), a.Role) {
		return fmt.Errorf("The role %s is not a role of this builder", a.Role)
	}
	if strings.TrimSpace(a.Message) == "" {
		return fmt.Errorf("The alert needs a message")
	}
	return nil
}

func (b *Build) publishAlert(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	a := record.(*Alert)
	if err := b.checkAlert(*a); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if err := freezeLegacyAlert(a); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	asset, err := alertReleaseAsset(*a, a.Version+1)
	if err == nil {
		err = retainAlertAsset(a, asset)
	}
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return nil
}

func alertReleaseAsset(a Alert, version int) (platform.ReleaseAsset, error) {
	if version < 1 || version > 64 {
		return platform.ReleaseAsset{}, fmt.Errorf("an alert may retain at most 64 published versions")
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetAlert, Name: a.Name}, ContractVersion: 1, SourceVersion: ValueDefinitionVersion + ".alert-" + strconv.Itoa(version), Body: platform.Raw(a.Definition())})
}

func sameRuleAsset(left, right platform.ReleaseAsset) bool {
	var a, b any
	return json.Unmarshal(platform.Raw(left), &a) == nil && json.Unmarshal(platform.Raw(right), &b) == nil && reflect.DeepEqual(a, b)
}

func alertFromAsset(asset platform.ReleaseAsset) (Alert, int, error) {
	var d platform.AlertRule
	text, found := strings.CutPrefix(asset.SourceVersion, ValueDefinitionVersion+".alert-")
	version, err := strconv.Atoi(text)
	if !found || err != nil || strconv.Itoa(version) != text || version < 1 || version > 64 || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetAlert || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &d) != nil || d.Name != asset.Ref.Name || d.Check() != nil || platform.CheckAssetDependencies(asset) != nil {
		return Alert{}, 0, fmt.Errorf("invalid alert asset %s", asset.Ref)
	}
	return Alert{Name: d.Name, Title: d.Title, Object: d.Object.Name, Field: d.Field, Operator: d.Operator, Value: d.Value, Message: d.Message, Role: d.Role, Every: d.Every, Active: d.Active}, version, nil
}

func legacyAlert(a Alert) bool {
	return a.State == "" && a.Version == 0 && a.Published == ""
}

func freezeLegacyAlert(a *Alert) error {
	if !legacyAlert(*a) {
		return nil
	}
	asset, err := alertReleaseAsset(*a, 1)
	if err != nil {
		return err
	}
	return retainAlertAsset(a, asset)
}

func retainAlertAsset(a *Alert, asset platform.ReleaseAsset) error {
	_, version, err := alertFromAsset(asset)
	if err != nil {
		return err
	}
	raw := string(platform.Raw(asset))
	if !slices.Contains(a.Versions, raw) {
		a.Versions = append(slices.Clone(a.Versions), raw)
	}
	a.Version = max(a.Version, version)
	a.DefinitionVersion = version
	a.State, a.Published = "published", raw
	return nil
}

func alertImage(image []byte) (Alert, platform.ReleaseAsset, error) {
	var a Alert
	var asset platform.ReleaseAsset
	if json.Unmarshal(image, &a) != nil || a.ID == "" || a.State != "published" || json.Unmarshal([]byte(a.Published), &asset) != nil {
		return a, asset, fmt.Errorf("accepted alert has no immutable publication")
	}
	frozen, version, err := alertFromAsset(asset)
	if err != nil || frozen.Name != a.Name || version != a.DefinitionVersion || version > a.Version || a.Version > 64 {
		return a, asset, fmt.Errorf("accepted alert identity differs from its publication")
	}
	return a, asset, nil
}

func (b *Build) alertAssets() ([]platform.ReleaseAsset, error) {
	rows, err := readDefinitionInventory[Alert](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return nil, err
	}
	var assets []platform.ReleaseAsset
	for _, a := range rows {
		if a.Archived {
			continue
		}
		if err := freezeLegacyAlert(&a); err != nil {
			return nil, err
		}
		if a.Published == "" {
			continue
		}
		_, asset, err := alertImage(platform.Raw(a))
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func (b *Build) AlertRules(object string) ([]Alert, error) {
	rows, err := readDefinitionInventory[Alert](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return nil, err
	}
	var rules []Alert
	for _, record := range rows {
		if record.Archived {
			continue
		}
		if err := freezeLegacyAlert(&record); err != nil {
			return nil, err
		}
		if record.Published == "" {
			continue
		}
		_, asset, err := alertImage(platform.Raw(record))
		if err != nil {
			return nil, err
		}
		a, _, err := alertFromAsset(asset)
		if err != nil {
			return nil, err
		}
		if a.Active && a.Object == object {
			a.Record = record.Record
			rules = append(rules, a)
		}
	}
	return rules, nil
}

func (b *Build) alertPinned(ref platform.AssetRef, version string) (platform.ReleaseAsset, error) {
	rows, err := readDefinitionInventory[Alert](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, record := range rows {
		if err := freezeLegacyAlert(&record); err != nil {
			return platform.ReleaseAsset{}, err
		}
		for _, raw := range append(slices.Clone(record.Versions), record.Published) {
			var asset platform.ReleaseAsset
			if json.Unmarshal([]byte(raw), &asset) == nil && asset.Ref == ref && asset.SourceVersion == version {
				_, _, err := alertFromAsset(asset)
				return asset, err
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("alert %s@%s is not retained", ref, version)
}

func (b *Build) alertDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	a, ok := platform.Get[Alert](b.host.Automation(platform.Caller{}, ID), id)
	if !ok || a.Archived {
		err = fmt.Errorf("alert draft %s was not found", id)
		return
	}
	if err = freezeLegacyAlert(&a); err != nil {
		return
	}
	if a.Published != "" {
		_, asset, problem := alertImage(platform.Raw(a))
		if problem != nil {
			err = problem
			return
		}
		prior, hadPrior = asset.Ref, true
	}
	if err = b.checkAlert(a); err != nil {
		return
	}
	next = platform.AssetRef{App: ID, Kind: platform.AssetAlert, Name: a.Name}
	asset, problem := alertReleaseAsset(a, a.Version+1)
	if problem != nil {
		err = problem
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(asset platform.ReleaseAsset) bool { return asset.Ref == next || hadPrior && asset.Ref == prior })
	after = append(after, asset)
	return
}

func (b *Build) prepareAlertPublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	rows, err := readDefinitionInventory[Alert](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return nil, err
	}
	var out []platform.ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetAlert {
			continue
		}
		frozen, version, err := alertFromAsset(asset)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(a Alert) bool { return !a.Archived && a.Name == frozen.Name })
		if i < 0 {
			return nil, fmt.Errorf("alert %s has no target draft", asset.Ref)
		}
		record := rows[i]
		if err := freezeLegacyAlert(&record); err != nil {
			return nil, err
		}
		if version <= record.Version {
			retained, err := b.alertPinned(asset.Ref, asset.SourceVersion)
			if err != nil || !sameRuleAsset(retained, asset) {
				return nil, fmt.Errorf("retained alert bytes differ")
			}
		} else if record.Version != 0 && version != record.Version+1 {
			return nil, fmt.Errorf("alert needs its next version or retained bytes")
		}
		if err := retainAlertAsset(&record, asset); err != nil {
			return nil, err
		}
		out = append(out, platform.ReleasePublication{Schema: SchemaAlertPublish, Image: platform.Raw(record)})
	}
	return out, nil
}

func (b *Build) checkConstraints(o Object, engines ...platform.RuleEngine) error {
	if len(o.Constraints) == 0 {
		return nil
	}
	info, err := platform.Describe(ID, Entity(o), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	lookup := ruleLookup(o, b.conditionLookup)
	if err := platform.CheckConstraints(info, lookup, true, engines...); err != nil {
		return err
	}
	for _, constraint := range o.Constraints {
		if err := b.checkRuleCollections(platform.ConstraintSpec(info, constraint), "constraint "+constraint.Name, lookup); err != nil {
			return err
		}
	}
	return nil
}

const AlertType = "build.alertrule"

type Alert struct {
	platform.Record
	Name              string   `json:"name" field:"required,search"`
	Title             string   `json:"title" field:"required,search"`
	Object            string   `json:"object" field:"required" title:"Object" help:"A published object, build.<name>"`
	Field             string   `json:"field" field:"required"`
	Operator          string   `json:"operator" field:"required" choices:"=,!=,<,<=,>,>=,empty,not empty"`
	Value             string   `json:"value,omitempty"`
	Message           string   `json:"message" field:"required" type:"longtext" help:"What the people told should know or do"`
	Role              string   `json:"role,omitempty" title:"Tell" help:"A role of this builder; empty: the builder"`
	Every             bool     `json:"every,omitempty" title:"Every time" help:"Tell again on every change that matches; default once per record"`
	Active            bool     `json:"active" title:"Active"`
	State             string   `json:"state,omitempty" field:"readonly"`
	Version           int      `json:"version,omitempty" field:"readonly"`
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly"`
	Versions          []string `json:"versions,omitempty" field:"readonly" type:"json"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"json"`
}

func (a Alert) Definition() platform.AlertRule {
	return platform.AlertRule{Name: a.Name, Title: a.Title, Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: a.Object}, Field: a.Field, Operator: a.Operator, Value: a.Value, Message: a.Message, Role: a.Role, Every: a.Every, Active: a.Active}
}

const (
	Integrator = "integrator"
)

var Markings = []string{"", "internal", "confidential", "restricted"}

func MarkingRank(m string) int { return max(slices.Index(Markings, m), 0) }

func HigherMarking(a, b string) string {
	if MarkingRank(b) > MarkingRank(a) {
		return b
	}
	return a
}

func Guarded(marking string) bool { return MarkingRank(marking) >= MarkingRank("confidential") }

func UnguardedFields(info platform.EntityInfo, columns []string) []string {
	var open []string
	for _, f := range info.Fields {
		if len(f.Read) == 0 && (columns == nil || slices.Contains(columns, f.Name)) {
			open = append(open, f.Name)
		}
	}
	return open
}
