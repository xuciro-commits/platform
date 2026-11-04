package authoring

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

const (
	AlertType          = "build.alertrule"
	SchemaAlertPublish = AlertType + ".publish"
)

type Alert struct {
	platform.Record
	Migrated          string   `json:"migrated,omitempty" field:"readonly"`
	MigratedVersion   int      `json:"migratedVersion,omitempty" field:"readonly"`
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

func (o *Owner) AlertEntity() platform.Entity {
	return platform.Entity{Type: AlertType, Title: "Legacy alert", Plural: "Legacy alerts", Model: Alert{}, Display: "title", Scope: platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}}}
}

func (o *Owner) checkAlert(c platform.Caller, a Alert) error {
	if !ontology.Named(a.Name) {
		return fmt.Errorf("The alert name %s must be lower-case letters and digits", a.Name)
	}
	info, ok := o.env.Entity(a.Object)
	if !ok {
		objects, err := o.env.Objects(c)
		if err != nil {
			return err
		}
		for _, object := range objects {
			if object.Type == a.Object {
				var describeErr error
				info, describeErr = platform.Describe(o.env.AppID(), object, func(reflect.Type) string { return "" })
				ok = describeErr == nil
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
	if !slices.Contains(ontology.Operators, a.Operator) {
		return fmt.Errorf("%q is not an operator", a.Operator)
	}
	if a.Role != "" && !slices.Contains(o.env.Roles(), a.Role) {
		return fmt.Errorf("The role %s is not a role of this builder", a.Role)
	}
	if strings.TrimSpace(a.Message) == "" {
		return fmt.Errorf("The alert needs a message")
	}
	return nil
}

func (o *Owner) alertReleaseAsset(a Alert, version int) (platform.ReleaseAsset, error) {
	if version < 1 || version > 64 {
		return platform.ReleaseAsset{}, fmt.Errorf("an alert may retain at most 64 published versions")
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetAlert, Name: a.Name}, ContractVersion: 1, SourceVersion: o.env.Version() + ".alert-" + strconv.Itoa(version), Body: platform.Raw(o.alertDefinition(a))})
}

func (o *Owner) alertDefinition(a Alert) platform.AlertRule {
	return platform.AlertRule{Name: a.Name, Title: a.Title, Object: platform.AssetRef{App: o.env.AppID(), Kind: platform.AssetObject, Name: a.Object}, Field: a.Field, Operator: a.Operator, Value: a.Value, Message: a.Message, Role: a.Role, Every: a.Every, Active: a.Active}
}

func (o *Owner) alertFromAsset(asset platform.ReleaseAsset) (Alert, int, error) {
	var d platform.AlertRule
	text, found := strings.CutPrefix(asset.SourceVersion, o.env.Version()+".alert-")
	version, err := strconv.Atoi(text)
	if !found || err != nil || strconv.Itoa(version) != text || version < 1 || version > 64 || asset.Ref.App != o.env.AppID() || asset.Ref.Kind != platform.AssetAlert || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &d) != nil || d.Name != asset.Ref.Name || d.Check() != nil || platform.CheckAssetDependencies(asset) != nil {
		return Alert{}, 0, fmt.Errorf("invalid alert asset %s", asset.Ref)
	}
	return Alert{Name: d.Name, Title: d.Title, Object: d.Object.Name, Field: d.Field, Operator: d.Operator, Value: d.Value, Message: d.Message, Role: d.Role, Every: d.Every, Active: d.Active}, version, nil
}

func legacyAlert(a Alert) bool {
	return a.State == "" && a.Version == 0 && a.Published == ""
}

func (o *Owner) freezeLegacyAlert(a *Alert) error {
	if !legacyAlert(*a) {
		return nil
	}
	asset, err := o.alertReleaseAsset(*a, 1)
	if err != nil {
		return err
	}
	return o.retainAlertAsset(a, asset)
}

func (o *Owner) retainAlertAsset(a *Alert, asset platform.ReleaseAsset) error {
	_, version, err := o.alertFromAsset(asset)
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

func (o *Owner) alertImage(image []byte) (Alert, platform.ReleaseAsset, error) {
	var a Alert
	var asset platform.ReleaseAsset
	if json.Unmarshal(image, &a) != nil || a.ID == "" || a.State != "published" || json.Unmarshal([]byte(a.Published), &asset) != nil {
		return a, asset, fmt.Errorf("accepted alert has no immutable publication")
	}
	frozen, version, err := o.alertFromAsset(asset)
	if err != nil || frozen.Name != a.Name || version != a.DefinitionVersion || version > a.Version || a.Version > 64 {
		return a, asset, fmt.Errorf("accepted alert identity differs from its publication")
	}
	return a, asset, nil
}

func (o *Owner) AcceptedAlert(asset platform.ReleaseAsset) (Alert, int, error) {
	alert, version, err := o.alertFromAsset(asset)
	if err != nil {
		return Alert{}, 0, err
	}
	alert.Record.ID = asset.Ref.Name
	alert.State, alert.Version, alert.DefinitionVersion = "published", version, version
	alert.Published = string(platform.Raw(asset))
	return alert, version, nil
}

func sameRuleAsset(left, right platform.ReleaseAsset) bool {
	var a, b any
	return json.Unmarshal(platform.Raw(left), &a) == nil && json.Unmarshal(platform.Raw(right), &b) == nil && reflect.DeepEqual(a, b)
}
