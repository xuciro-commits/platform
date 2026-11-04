package ontology

import (
	"fmt"
	"slices"

	"platformserver/platform"
)

type Object struct {
	platform.Record
	Name              string                       `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"visit"`
	Title             string                       `json:"title" field:"required,search" title:"What people call it" example:"Visit"`
	Plural            string                       `json:"plural,omitempty" title:"What people call several" example:"Visits"`
	Icon              string                       `json:"icon,omitempty" title:"Icon" help:"An icon name, lower-case with hyphens" example:"calendar"`
	Color             string                       `json:"color,omitempty" title:"Color" choices:"neutral,info,success,warning,danger,purple,pink,teal,orange"`
	Display           string                       `json:"display,omitempty" title:"Title field" help:"The text field that names one record; empty: the first searchable field" example:"name"`
	ObjectView        *platform.ObjectView         `json:"objectView,omitempty" type:"json" title:"Object view"`
	Description       string                       `json:"description,omitempty" type:"longtext" help:"What one record of it is, for people and agents"`
	Fields            []Field                      `json:"fields" title:"Fields"`
	DataSources       []platform.DataSourceBinding `json:"datasources,omitempty" type:"json" field:"aside" title:"Data sources"`
	Protocols         []platform.ProtocolBinding   `json:"protocols,omitempty" type:"json" field:"aside" title:"Protocol bindings"`
	Reads             *platform.RuleReadSpec       `json:"reads,omitempty" type:"json" field:"aside"`
	Constraints       []platform.Constraint        `json:"constraints,omitempty" type:"json" field:"aside" title:"Business constraints"`
	Sets              []platform.RecordSet         `json:"sets,omitempty" type:"json" field:"aside" title:"Declared record sets"`
	DispatchCommands  []platform.DispatchCommand   `json:"dispatchCommands,omitempty" type:"json" field:"aside" title:"Device commands"`
	CreateTransaction *platform.BusinessTxn        `json:"createTransaction,omitempty" type:"json" field:"aside" title:"Creation transaction"`
	Versioning        *platform.Versioning         `json:"versioning,omitempty" type:"json" field:"aside" title:"Business versioning"`
	VersionPins       []platform.VersionPinBinding `json:"versionPins,omitempty" type:"json" field:"aside" title:"Pinned business versions"`
	EnterpriseUse     *platform.Expr               `json:"enterpriseUse,omitempty" type:"json" field:"aside" title:"Current enterprise use"`
	States            []State                      `json:"states,omitempty" field:"aside" title:"States"`
	ActionTypes       []platform.AssetBinding      `json:"actionTypes,omitempty" field:"aside" type:"json" title:"Action types"`
	Actions           []Action                     `json:"actions,omitempty" field:"aside" title:"Actions"`
	Access            []Access                     `json:"access,omitempty" field:"aside" title:"Access"`
	Scope             *ObjectScope                 `json:"scope,omitempty" type:"json" field:"aside" title:"Scope fields"`
	Implements        []string                     `json:"implements,omitempty" field:"aside" title:"Implements" help:"Interfaces whose fields this object has, e.g. core.coded"`
	Extends           string                       `json:"extends,omitempty" field:"aside" title:"Extends" help:"The installed object type this object adds fields to, e.g. core.person; records pair one to one through the base field"`
	ExtensionSource   *platform.ExtensionSource    `json:"extensionSource,omitempty" field:"readonly,aside" type:"json" title:"Extension source" help:"The original base package version and field shape captured at publication"`
	SchemaVersions    []platform.SchemaVersion     `json:"schemaVersions,omitempty" field:"readonly,aside" type:"json" title:"Schema versions" help:"Retained content versions and compatible schema families captured at publication"`
	Numbering         *Numbering                   `json:"numbering,omitempty" field:"aside" type:"json" title:"Document numbering"`
	State             string                       `json:"state" field:"readonly" choices:"draft,published"`
	Installed         string                       `json:"installed,omitempty" field:"readonly" title:"Installed as" help:"The type records of it are stored under"`
	Published         string                       `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
}

type Field struct {
	AIInput     *bool                  `json:"aiInput,omitempty" title:"Allow AI context"`
	SourceType  string                 `json:"sourceType,omitempty" choices:"record,user-input,external-input,generated" title:"Text source type"`
	Property    *platform.AssetBinding `json:"property,omitempty" type:"json" field:"aside"`
	ValueType   *platform.AssetBinding `json:"valueType,omitempty" type:"json" field:"aside"`
	Format      []platform.FormatRule  `json:"format,omitempty" type:"json" field:"aside"`
	Name        string                 `json:"name" field:"required" help:"Lower-case letters and digits" example:"visited"`
	Title       string                 `json:"title" field:"required" title:"Label" example:"Visited on"`
	Type        string                 `json:"type" field:"required" choices:"text,longtext,integer,decimal,money,date,datetime,boolean,choice,reference,attachment" example:"date"`
	Choices     string                 `json:"choices,omitempty" help:"For a choice: the values, comma-separated" example:"open,done"`
	Ref         string                 `json:"ref,omitempty" title:"Refers to" help:"For a reference: the object it points at" example:"core.partner"`
	Stereotype  string                 `json:"stereotype,omitempty" title:"Enterprise element type"`
	Stereotypes []string               `json:"stereotypes,omitempty" title:"Enterprise element types"`
	Inverse     string                 `json:"inverse,omitempty" title:"Seen from there as" help:"For a reference: what the referenced record calls these records" example:"visits"`
	Required    bool                   `json:"required,omitempty"`
	Search      bool                   `json:"search,omitempty" title:"Searchable"`
	ReadOnly    bool                   `json:"readOnly,omitempty" title:"Read only"`
	AliasOf     string                 `json:"aliasOf,omitempty" title:"Read alias of" help:"The retained old name reads this field; it cannot be written directly"`
	When        string                 `json:"when,omitempty" title:"Only when" help:"field=value,value — another choice or boolean field of this object and the values that make this field apply" example:"kind=refund,return"`
	FormulaExpr *platform.Expr         `json:"formulaExpr,omitempty" type:"json" field:"aside"`
	Formula     string                 `json:"formula,omitempty" help:"For a number: price * qty, (ordered - received), …" example:"price * qty"`
	Read        []string               `json:"read,omitempty" title:"Read by" help:"Roles that read it; empty: every role that reads the object"`
	Write       []string               `json:"write,omitempty" title:"Set by" help:"Roles that set it; empty: every role that edits the object"`
}

type State struct {
	Name        string `json:"name" field:"required" help:"Lower-case letters and digits" example:"found"`
	Title       string `json:"title" field:"required" title:"What people call it" example:"Found"`
	Tone        string `json:"tone,omitempty" choices:"info,success,warning,danger,neutral"`
	Description string `json:"description,omitempty" help:"What a record in this state means"`
}

type Action struct {
	ConfirmationOnly bool                       `json:"confirmationOnly,omitempty" title:"Require original confirmation receipt"`
	Adoption         *platform.ProposalAdoption `json:"adoption,omitempty" type:"json" field:"aside"`
	Name             string                     `json:"name" field:"required" help:"Lower-case letters and digits" example:"return"`
	Title            string                     `json:"title" field:"required" title:"What people call it" example:"Hand it back"`
	Description      string                     `json:"description,omitempty" help:"What it does, for people and agents"`
	From             []string                   `json:"from" title:"Taken from" help:"The states it takes a record from"`
	ToInput          string                     `json:"toInput,omitempty" title:"State from input"`
	To               string                     `json:"to,omitempty" title:"Leaves it in" help:"The state it leaves the record in; empty: where it was"`
	Inputs           []Input                    `json:"inputs,omitempty" title:"What people give"`
	Transaction      *platform.BusinessTxn      `json:"transaction,omitempty"`
	Reads            *platform.RuleReadSpec     `json:"reads,omitempty" type:"json" field:"aside"`
	Sets             []Set                      `json:"sets,omitempty" title:"What it sets"`
	Conditions       []Condition                `json:"conditions,omitempty" title:"What it needs"`
	Roles            []string                   `json:"roles,omitempty" title:"Taken by"`
	Approval         *ActionApproval            `json:"approval,omitempty" title:"Approval"`
	Creates          []Create                   `json:"creates,omitempty" title:"What it creates"`
	Posts            []Post                     `json:"posts,omitempty" title:"What it posts"`
	Journal          *JournalPost               `json:"journal,omitempty" title:"What it books"`
	Reverses         string                     `json:"reverses,omitempty" title:"Reverses action"`
}

type JournalPost struct {
	ClosedPeriod string            `json:"closedPeriod,omitempty" choices:"reject,next-open" title:"Closed period policy"`
	Date         string            `json:"date,omitempty" example:"record.receivedOn"`
	Text         string            `json:"text,omitempty" help:"A literal or a source for the entry's text"`
	Lines        []JournalPostLine `json:"lines" title:"Lines"`
}

type JournalPostLine struct {
	Account string `json:"account" help:"=<account code> or a source holding one" example:"=1403"`
	Debit   string `json:"debit,omitempty" example:"amount"`
	Credit  string `json:"credit,omitempty" example:"amount"`
	Text    string `json:"text,omitempty"`
	Object  string `json:"object,omitempty" help:"What the line is about, e.g. record.material"`
	Partner string `json:"partner,omitempty" example:"record.supplier"`
}

type Create struct {
	Object string `json:"object" field:"required" help:"A defined object of this builder whose reference points at this one" example:"build.followup"`
	Via    string `json:"via" field:"required" help:"That object's reference field to this record" example:"visit"`
	Sets   []Set  `json:"sets,omitempty" title:"What it sets"`
}

type ActionApproval struct {
	Pending  string          `json:"pending" title:"While it waits"`
	Rejected string          `json:"rejected,omitempty" title:"If rejected"`
	Levels   []ApproverLevel `json:"levels" title:"Approver levels"`
}

type ApproverLevel struct {
	Title string `json:"title" title:"What this level is called"`
	Role  string `json:"role" title:"Approver role"`
	All   bool   `json:"all,omitempty" title:"Everyone in the role must approve"`
}

type Input struct {
	Constraints *platform.InputConstraints `json:"constraints,omitempty"`
	Range       *platform.DateRange        `json:"range,omitempty"`
	Group       string                     `json:"group,omitempty"`
	Ref         string                     `json:"ref,omitempty" title:"Reference object"`
	Stereotype  string                     `json:"stereotype,omitempty" title:"Enterprise element type"`
	Stereotypes []string                   `json:"stereotypes,omitempty" title:"Enterprise element types"`
	MinLength   *int                       `json:"minLength,omitempty" title:"Minimum length"`
	Name        string                     `json:"name" field:"required" example:"to"`
	Title       string                     `json:"title" field:"required" title:"Label" example:"Handed to"`
	Type        string                     `json:"type" field:"required" choices:"text,longtext,integer,decimal,date,datetime,boolean,choice,reference,attachment"`
	Choices     string                     `json:"choices,omitempty" help:"For a choice: the values, comma-separated"`
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
	return platform.Field{Name: in.Name, Type: ValueInputTypes[in.Type], Ref: in.Ref, Stereotype: in.Stereotype, Stereotypes: in.Stereotypes, Required: in.Required, Description: in.Title, Choices: ValueChoices(in.Choices), Constraints: &c, Range: in.Range, Group: in.Group}
}

type Set = platform.TxnSet

type Condition struct {
	Expr       *platform.Expr `json:"expr,omitempty" type:"json" field:"aside"`
	When       *Condition     `json:"when,omitempty" title:"Only when"`
	Field      string         `json:"field,omitempty" help:"A declared record path through references, or input.<name>"`
	Operator   string         `json:"operator,omitempty" choices:"=,!=,<,<=,>,>=,empty,not empty"`
	Value      string         `json:"value,omitempty" help:"What it is compared with; $me is the person taking the action"`
	ValueField string         `json:"valueField,omitempty" help:"Compare with another declared record path or input; excludes value"`
	Message    string         `json:"message" field:"required" help:"What a person reads when it does not hold"`
}

type Access struct {
	Role    string `json:"role" field:"required" help:"A role of the builder app, lower-case letters and digits" example:"desk"`
	Read    string `json:"read" field:"required" choices:"all,below,unit,own,none" help:"all: every record; below: those of the member's units and the units under them; unit: those of the member's own units; own: their own; none: not the object at all"`
	Create  bool   `json:"create,omitempty"`
	Edit    bool   `json:"edit,omitempty"`
	Archive bool   `json:"archive,omitempty"`
}

type ObjectScope struct {
	Owner     string `json:"owner,omitempty" help:"The field holding the member a record belongs to; empty: whoever created it"`
	Unit      string `json:"unit,omitempty" help:"The field holding the record's organisation (a reference to enterprise.element, or its id)"`
	Structure string `json:"structure,omitempty" help:"The relationship kind \"below\" follows in the enterprise model, e.g. management"`
}

type Numbering struct {
	Field  string `json:"field" help:"The text field that takes the number" example:"number"`
	Prefix string `json:"prefix,omitempty" example:"GR"`
	Yearly bool   `json:"yearly,omitempty" help:"Restart at 1 every year and put the year in the number"`
	Width  int    `json:"width,omitempty" help:"Digits, zero-padded; default 6"`
}

func (n Numbering) Check(o Object) error {
	if n.Field == "" {
		return fmt.Errorf("the numbering names the field that takes the number")
	}
	for _, f := range o.Fields {
		if f.Name == n.Field {
			if f.Type != "text" {
				return fmt.Errorf("the numbering field %q is not a text field", n.Field)
			}
			return nil
		}
	}
	return fmt.Errorf("the numbering field %q is not a field of the object", n.Field)
}

func (n Numbering) Format(seq, year int) string {
	width := n.Width
	if width <= 0 {
		width = 6
	}
	if n.Yearly {
		return fmt.Sprintf("%s%d-%0*d", n.Prefix, year, width, seq)
	}
	return fmt.Sprintf("%s%0*d", n.Prefix, width, seq)
}

type Post struct {
	Object   string `json:"object" field:"required" help:"A defined object of this builder that keeps the balance" example:"build.stockbalance"`
	Match    []Set  `json:"match" title:"Balance identified by"`
	Field    string `json:"field" field:"required" help:"The balance's integer, decimal or money field" example:"onhand"`
	Amount   string `json:"amount" field:"required" example:"quantity"`
	Subtract bool   `json:"subtract,omitempty" title:"Subtract"`
	Floor    bool   `json:"floor,omitempty" title:"Never below zero"`
}

func (a Action) Reversal(of Action) []Post {
	posts := make([]Post, len(of.Posts))
	for i, p := range of.Posts {
		p.Subtract, p.Floor = !p.Subtract, false
		posts[i] = p
	}
	return posts
}

func (o Object) EffectivePosts(a Action) []Post {
	if a.Reverses == "" {
		return a.Posts
	}
	for _, of := range o.Actions {
		if of.Name == a.Reverses {
			return a.Reversal(of)
		}
	}
	return nil
}

type PropertyType struct {
	platform.Record
	Name        string   `json:"name" field:"required,search"`
	Title       string   `json:"title" field:"required,search"`
	Description string   `json:"description" field:"required" type:"longtext"`
	Type        string   `json:"type" field:"required" choices:"text,longtext,integer,decimal,date,datetime,boolean,attachment"`
	State       string   `json:"state" field:"readonly" choices:"draft,published"`
	Version     int      `json:"version,omitempty" field:"readonly"`
	Published   string   `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions    []string `json:"versions,omitempty" field:"readonly"`
}

func (f PropertyType) Definition() platform.PropertyType {
	return platform.PropertyType{Name: f.Name, Title: f.Title, Description: f.Description, Type: f.Type}
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
	Published   string                    `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions    []string                  `json:"versions,omitempty" field:"readonly"`
}

func (f ValueType) Definition() platform.ValueType {
	return platform.ValueType{Name: f.Name, Title: f.Title, Description: f.Description, Base: f.Base, Constraints: f.Constraints, Pattern: f.Pattern, Choices: slices.Clone(f.Choices)}
}

type ObjectTypeGroup struct {
	platform.Record
	Name        string              `json:"name" field:"required,search"`
	Title       string              `json:"title" field:"required,search"`
	Description string              `json:"description,omitempty" type:"longtext"`
	Members     []platform.AssetRef `json:"members" type:"json"`
	State       string              `json:"state" field:"readonly" choices:"draft,published"`
	Version     int                 `json:"version,omitempty" field:"readonly"`
	Published   string              `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions    []string            `json:"versions,omitempty" field:"readonly"`
}

func (g ObjectTypeGroup) Definition() platform.ObjectTypeGroup {
	return platform.ObjectTypeGroup{Name: g.Name, Title: g.Title, Description: g.Description, Members: append([]platform.AssetRef{}, g.Members...)}
}
