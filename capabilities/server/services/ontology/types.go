package ontology

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"
	"slices"
	"strings"

	"platformserver/platform"
)

type Object struct {
	platform.Record
	Ledger               bool                         `json:"ledger,omitempty" title:"Ledger"`
	ClosedBefore         string                       `json:"closedBefore,omitempty" type:"date" title:"Closed before"`
	Balances             []Balance                    `json:"balances,omitempty" type:"json" field:"aside"`
	Name                 string                       `json:"name" field:"required,search" title:"API name" help:"Its name in the platform, lower-case letters and digits" example:"visit"`
	Namespace            string                       `json:"namespace,omitempty" title:"Application" help:"The project whose application owns this type; set from the project when the type is created"`
	Title                string                       `json:"title" field:"required,search" title:"Name" help:"What people call it" example:"Visit"`
	Plural               string                       `json:"plural,omitempty" title:"Plural" help:"What people call several" example:"Visits"`
	Icon                 string                       `json:"icon,omitempty" title:"Icon" help:"An icon name, lower-case with hyphens" example:"calendar"`
	Color                string                       `json:"color,omitempty" title:"Color" choices:"neutral,info,success,warning,danger,purple,pink,teal,orange"`
	Display              string                       `json:"display,omitempty" title:"Title field" help:"The text field that names one record; empty: the first searchable field" example:"name"`
	Naming               string                       `json:"naming,omitempty" title:"Naming pattern" help:"Automatic record number pattern, e.g. CRM-LEAD-{year}-{n:5}" example:"CRM-LEAD-{year}-{n:5}"`
	ObjectView           *platform.ObjectView         `json:"objectView,omitempty" type:"json" title:"Object view"`
	View                 json.RawMessage              `json:"view,omitempty" type:"json" title:"Default view"`
	Description          string                       `json:"description,omitempty" type:"longtext" help:"What one record of it is, for people and agents"`
	Fields               []Field                      `json:"fields" title:"Fields"`
	DataSources          []platform.DataSourceBinding `json:"datasources,omitempty" type:"json" field:"aside" title:"Data sources"`
	Protocols            []protocol.ProtocolBinding   `json:"protocols,omitempty" type:"json" field:"aside" title:"Protocol bindings"`
	Reads                *platform.RuleReadSpec       `json:"reads,omitempty" type:"json" field:"aside"`
	Constraints          []platform.Constraint        `json:"constraints,omitempty" type:"json" field:"aside" title:"Business constraints"`
	Sets                 []platform.RecordSet         `json:"sets,omitempty" type:"json" field:"aside" title:"Record sets" help:"Declared record sets"`
	DispatchCommands     []platform.DispatchCommand   `json:"dispatchCommands,omitempty" type:"json" field:"aside" title:"Device commands"`
	CreateTransaction    *platform.BusinessTxn        `json:"createTransaction,omitempty" type:"json" field:"aside" title:"Creation transaction"`
	Versioning           *platform.Versioning         `json:"versioning,omitempty" type:"json" field:"aside" title:"Business versioning"`
	VersionPins          []platform.VersionPinBinding `json:"versionPins,omitempty" type:"json" field:"aside" title:"Version pins" help:"Pinned business versions"`
	EnterpriseUse        *platform.Expr               `json:"enterpriseUse,omitempty" type:"json" field:"aside" title:"Enterprise use" help:"Current enterprise use"`
	States               []State                      `json:"states,omitempty" rail:"true" title:"States"`
	ActionTypes          []platform.AssetBinding      `json:"actionTypes,omitempty" rail:"true" type:"json" title:"Action types"`
	Actions              []Action                     `json:"actions,omitempty" field:"aside" title:"Actions"`
	Ask                  map[string]Ask               `json:"ask,omitempty" field:"aside" type:"json" title:"Ask first"`
	Records              []json.RawMessage            `json:"records,omitempty" type:"json" field:"aside" title:"Reference records" help:"Rows shipped with the type, such as the stages of a pipeline; a publish or an install adds the ones this workspace lacks"`
	Access               []Access                     `json:"access,omitempty" rail:"true" title:"Access"`
	Scope                *ObjectScope                 `json:"scope,omitempty" type:"json" field:"aside" title:"Scope fields"`
	Implements           []string                     `json:"implements,omitempty" field:"aside" title:"Implements" help:"Interfaces whose fields this object has, e.g. core.coded"`
	Extends              string                       `json:"extends,omitempty" field:"aside" title:"Extends" help:"The installed object type this object adds fields to, e.g. core.person; records pair one to one through the base field"`
	ExtensionSource      *platform.ExtensionSource    `json:"extensionSource,omitempty" field:"readonly,aside" type:"json" title:"Extension source" help:"The original base package version and field shape captured at publication"`
	SchemaVersions       []platform.SchemaVersion     `json:"schemaVersions,omitempty" field:"readonly,aside" type:"json" title:"Schema versions" help:"Retained content versions and compatible schema families captured at publication"`
	RetainedFlowClosures map[string][]string          `json:"retainedFlowClosures,omitempty" field:"readonly,internal" type:"json"`
	State                string                       `json:"state" field:"readonly" choices:"draft,published"`
	Installed            string                       `json:"installed,omitempty" field:"readonly" title:"Installed as" help:"The type records of it are stored under"`
	Published            string                       `json:"published,omitempty" field:"readonly,internal" type:"longtext" title:"Installed" help:"What is installed"`
}

type Balance struct {
	platform.LedgerBalance
	Title  string   `json:"title"`
	Access []Access `json:"access,omitempty"`
}

type Field struct {
	AIInput     *bool                  `json:"aiInput,omitempty" title:"AI context" help:"Allow AI context"`
	SourceType  string                 `json:"sourceType,omitempty" choices:"record,user-input,external-input,generated" title:"Text source" help:"Text source type"`
	Property    *platform.AssetBinding `json:"property,omitempty" type:"json" field:"aside"`
	ValueType   *platform.AssetBinding `json:"valueType,omitempty" type:"json" field:"aside"`
	Format      []platform.FormatRule  `json:"format,omitempty" type:"json" field:"aside"`
	Name        string                 `json:"name" field:"required" help:"Lower-case letters and digits" example:"visited"`
	Title       string                 `json:"title" field:"required" title:"Label" example:"Visited on"`
	Group       string                 `json:"group,omitempty" title:"Section"`
	Aside       bool                   `json:"aside,omitempty" title:"Side rail"`
	Type        string                 `json:"type" field:"required" choices:"text,longtext,integer,decimal,money,date,datetime,boolean,choice,reference,member,attachment" example:"date"`
	Choices     string                 `json:"choices,omitempty" help:"For a choice: the values, comma-separated; value:Label gives one a display name" example:"open:Open,done:Done"`
	Ref         string                 `json:"ref,omitempty" title:"Refers to" help:"For a reference: the object it points at" example:"core.partner"`
	Stereotype  string                 `json:"stereotype,omitempty" title:"Element type" help:"Enterprise element type"`
	Stereotypes []string               `json:"stereotypes,omitempty" title:"Element types" help:"Enterprise element types"`
	Inverse     string                 `json:"inverse,omitempty" title:"Inverse name" help:"Seen from there as. For a reference: what the referenced record calls these records" example:"visits"`
	OneOf       string                 `json:"oneOf,omitempty" title:"One of" help:"For a reference: the name of a group of references to different objects, of which a record sets at most one" example:"Related to"`
	Required    bool                   `json:"required,omitempty"`
	Search      bool                   `json:"search,omitempty" title:"Searchable"`
	ReadOnly    bool                   `json:"readOnly,omitempty" title:"Read only"`
	AliasOf     string                 `json:"aliasOf,omitempty" title:"Read alias" help:"Read alias of. The retained old name reads this field; it cannot be written directly"`
	When        string                 `json:"when,omitempty" title:"Only when" help:"field=value,value — another choice or boolean field of this object and the values that make this field apply" example:"kind=refund,return"`
	Default     string                 `json:"default,omitempty" title:"Default" help:"Default value on creation: literal, today, now, now+<duration>, member, or setting:<name>"`
	Live        *platform.Expr         `json:"live,omitempty" type:"json" field:"aside"`
	FormulaExpr *platform.Expr         `json:"formulaExpr,omitempty" type:"json" field:"aside"`
	From        *platform.Expr         `json:"from,omitempty" type:"json" field:"aside"`
	Formula     string                 `json:"formula,omitempty" help:"For a number: price * qty, (ordered - received), …" example:"price * qty"`
	Read        []string               `json:"read,omitempty" title:"Read by" help:"Roles that read it; empty: every role that reads the object"`
	Write       []string               `json:"write,omitempty" title:"Set by" help:"Roles that set it; empty: every role that edits the object"`
}

func (o Object) Type() string {
	if o.Namespace != "" {
		return o.Namespace + "." + o.Name
	}
	return AppID + "." + o.Name
}

func (o Object) PageName() string {
	return strings.TrimPrefix(o.Type(), AppID+".")
}

func (o Object) ShownBy(page, object string) bool {
	return page == o.PageName() || page == o.Name && object == o.Type()
}

type State struct {
	Name        string `json:"name" field:"required" title:"API name" help:"Lower-case letters and digits" example:"found"`
	Title       string `json:"title" field:"required" title:"Name" help:"What people call it" example:"Found"`
	Tone        string `json:"tone,omitempty" choices:"info,success,warning,danger,neutral"`
	Description string `json:"description,omitempty" help:"What a record in this state means"`
}

type Ask struct {
	Enabled bool
	Reason  string
	Invalid string
}

func (a Ask) IsZero() bool { return !a.Enabled && a.Invalid == "" }

func (a *Ask) UnmarshalJSON(raw []byte) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "false" {
		*a = Ask{}
		return nil
	}
	if trimmed == "true" {
		*a = Ask{Enabled: true}
		return nil
	}
	var reason string
	if err := json.Unmarshal(raw, &reason); err == nil {
		clean := strings.TrimSpace(reason)
		if clean == "" {
			*a = Ask{Reason: reason, Invalid: "ask reason cannot be blank"}
			return nil
		}
		if len([]rune(clean)) > 500 {
			*a = Ask{Reason: clean, Invalid: "ask reason exceeds 500 characters"}
			return nil
		}
		*a = Ask{Enabled: true, Reason: clean}
		return nil
	}
	*a = Ask{Invalid: "ask must be true or a non-empty reason"}
	return nil
}

func (a Ask) MarshalJSON() ([]byte, error) {
	if a.Invalid != "" {
		return json.Marshal(a.Reason)
	}
	if a.Reason != "" {
		return json.Marshal(a.Reason)
	}
	return json.Marshal(a.Enabled)
}

func (a Ask) Check(where string) error {
	if a.Invalid != "" {
		return fmt.Errorf("%s: %s", where, a.Invalid)
	}
	return nil
}

type Action struct {
	ConfirmationOnly bool                       `json:"confirmationOnly,omitempty" title:"Confirmation receipt" help:"Require original confirmation receipt"`
	Ask              Ask                        `json:"ask,omitzero" title:"Ask first" help:"Hold AI calls in the requester's inbox until approved"`
	Adoption         *platform.ProposalAdoption `json:"adoption,omitempty" type:"json" field:"aside"`
	Name             string                     `json:"name" field:"required" title:"API name" help:"Lower-case letters and digits" example:"return"`
	Title            string                     `json:"title" field:"required" title:"Name" help:"What people call it" example:"Hand it back"`
	Description      string                     `json:"description,omitempty" help:"What it does, for people and agents"`
	From             []string                   `json:"from" title:"Taken from" help:"The states it takes a record from"`
	Inputs           []Input                    `json:"inputs,omitempty" title:"Inputs" help:"What people give"`
	Transaction      *platform.BusinessTxn      `json:"transaction,omitempty"`
	Open             string                     `json:"open,omitempty" title:"Opens" help:"A reference field of this object; after the action people land on the record it names, such as the one the action created"`
	Roles            []string                   `json:"roles,omitempty" title:"Taken by"`
	Approval         *ActionApproval            `json:"approval,omitempty" title:"Approval"`
	Uses             []string                   `json:"uses,omitempty" title:"Uses capabilities" help:"Capability contract actions this action needs, as name/major#action; it appears only while the tenant has bound an implementation"`
}

type ActionApproval struct {
	Pending  string          `json:"pending" title:"Pending state" help:"While it waits"`
	Rejected string          `json:"rejected,omitempty" title:"If rejected"`
	Levels   []ApproverLevel `json:"levels" title:"Approver levels"`
}

type ApproverLevel struct {
	Title   string `json:"title" title:"Level name" help:"What this level is called"`
	Role    string `json:"role,omitempty" title:"Approver role"`
	Manager bool   `json:"manager,omitempty" title:"Requester's manager" help:"The manager of the requester's own organization unit; with a role, holders of the role may approve too"`
	All     bool   `json:"all,omitempty" title:"Unanimous approval" help:"Everyone in the role must approve"`
}

type Input struct {
	Constraints *platform.InputConstraints `json:"constraints,omitempty"`
	Range       *platform.DateRange        `json:"range,omitempty"`
	Group       string                     `json:"group,omitempty"`
	Ref         string                     `json:"ref,omitempty" title:"Reference object"`
	Stereotype  string                     `json:"stereotype,omitempty" title:"Element type" help:"Enterprise element type"`
	Stereotypes []string                   `json:"stereotypes,omitempty" title:"Element types" help:"Enterprise element types"`
	MinLength   *int                       `json:"minLength,omitempty" title:"Minimum length"`
	Name        string                     `json:"name" field:"required" example:"to"`
	Title       string                     `json:"title" field:"required" title:"Label" example:"Handed to"`
	Help        string                     `json:"help,omitempty" help:"What to give, shown under the input" example:"Leave empty to create one from the lead"`
	Type        string                     `json:"type" field:"required" choices:"text,longtext,integer,decimal,date,datetime,boolean,choice,reference,attachment,money,member"`
	Choices     string                     `json:"choices,omitempty" help:"For a choice: the values, comma-separated; value:Label gives one a display name"`
	Required    bool                       `json:"required,omitempty"`
}

func (in Input) Field() platform.Field {
	c := platform.InputConstraints{}
	if in.Constraints != nil {
		c = *in.Constraints
	}
	if in.MinLength != nil {
		c.MinLength = in.MinLength
	}
	description := in.Title
	if in.Help != "" {
		description = in.Help
	}
	return platform.Field{Name: in.Name, Type: ValueInputTypes[in.Type], Ref: in.Ref, Stereotype: in.Stereotype, Stereotypes: in.Stereotypes, Required: in.Required, Label: in.Title, Description: description, Choices: ValueChoices(in.Choices), Constraints: &c, Range: in.Range, Group: in.Group}
}

type Set = platform.TxnSet

type Access struct {
	Role    string `json:"role" field:"required" help:"A role of the builder app, lower-case letters and digits" example:"desk"`
	Read    string `json:"read" field:"required" choices:"all,below,unit,team,own,none" help:"all: every record; below: those of the member's units and the units under them; unit: those of the member's own units; team: those of colleagues in the member's units; own: their own; none: not the object at all"`
	Create  bool   `json:"create,omitempty"`
	Edit    bool   `json:"edit,omitempty"`
	Archive bool   `json:"archive,omitempty"`
}

type ObjectScope struct {
	Owner     string `json:"owner,omitempty" help:"The field holding the member a record belongs to; empty: whoever created it"`
	Unit      string `json:"unit,omitempty" help:"The field holding the record's organisation (a reference to enterprise.element, or its id)"`
	Structure string `json:"structure,omitempty" help:"The relationship kind \"below\" follows in the enterprise model, e.g. management"`
	Assigned  bool   `json:"assigned,omitempty" help:"Leave the owner empty for assignment unless the creator could not see the record otherwise"`
}

type PropertyType struct {
	platform.Record
	Name        string   `json:"name" field:"required,search"`
	Title       string   `json:"title" field:"required,search"`
	Description string   `json:"description" field:"required" type:"longtext"`
	Type        string   `json:"type" field:"required" choices:"text,longtext,integer,decimal,date,datetime,boolean,attachment"`
	State       string   `json:"state" field:"readonly" choices:"draft,published"`
	Version     int      `json:"version,omitempty" field:"readonly"`
	Published   string   `json:"published,omitempty" field:"readonly,internal" type:"longtext"`
	Versions    []string `json:"versions,omitempty" field:"readonly"`
}

func (f PropertyType) Definition() platform.PropertyType {
	return platform.PropertyType{Name: f.Name, Title: f.Title, Description: f.Description, Type: f.Type}
}

func PropertyTypeFromDeclaration(property platform.PropertyType) PropertyType {
	return PropertyType{Name: property.Name, Title: property.Title, Description: property.Description, Type: property.Type}
}

type ValueType struct {
	platform.Record
	Name        string                    `json:"name" field:"required,search"`
	Title       string                    `json:"title" field:"required,search"`
	Description string                    `json:"description" field:"required" type:"longtext"`
	Base        string                    `json:"base" field:"required" choices:"text,longtext,integer,decimal,date,datetime"`
	Constraints platform.InputConstraints `json:"constraints,omitzero" type:"json" field:"aside"`
	Pattern     string                    `json:"pattern,omitempty" help:"An RE2 regular expression the text must match; anchor it with ^ and $ for the whole value"`
	Choices     []string                  `json:"choices,omitempty" field:"aside" title:"Listed values"`
	State       string                    `json:"state" field:"readonly" choices:"draft,published"`
	Version     int                       `json:"version,omitempty" field:"readonly"`
	Published   string                    `json:"published,omitempty" field:"readonly,internal" type:"longtext"`
	Versions    []string                  `json:"versions,omitempty" field:"readonly"`
}

func (f ValueType) Definition() platform.ValueType {
	return platform.ValueType{Name: f.Name, Title: f.Title, Description: f.Description, Base: f.Base, Constraints: f.Constraints, Pattern: f.Pattern, Choices: slices.Clone(f.Choices)}
}

func ValueTypeFromDeclaration(value platform.ValueType) ValueType {
	return ValueType{Name: value.Name, Title: value.Title, Description: value.Description, Base: value.Base, Constraints: value.Constraints, Pattern: value.Pattern, Choices: slices.Clone(value.Choices)}
}

type ObjectTypeGroup struct {
	platform.Record
	Name        string              `json:"name" field:"required,search"`
	Title       string              `json:"title" field:"required,search"`
	Description string              `json:"description,omitempty" type:"longtext"`
	Members     []platform.AssetRef `json:"members" type:"json"`
	State       string              `json:"state" field:"readonly" choices:"draft,published"`
	Version     int                 `json:"version,omitempty" field:"readonly"`
	Published   string              `json:"published,omitempty" field:"readonly,internal" type:"longtext"`
	Versions    []string            `json:"versions,omitempty" field:"readonly"`
}

func (g ObjectTypeGroup) Definition() platform.ObjectTypeGroup {
	return platform.ObjectTypeGroup{Name: g.Name, Title: g.Title, Description: g.Description, Members: append([]platform.AssetRef{}, g.Members...)}
}

func ObjectTypeGroupFromDeclaration(group platform.ObjectTypeGroup) ObjectTypeGroup {
	return ObjectTypeGroup{Name: group.Name, Title: group.Title, Description: group.Description, Members: slices.Clone(group.Members)}
}
