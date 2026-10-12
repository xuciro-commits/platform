package buildapi

import (
	"strings"

	"platformserver/platform"
)

const (
	ID                     = "build"
	ValueDefinitionVersion = "1"
	ObjectType             = "build.object"
	// Builder defines and publishes objects; Publisher saves and activates
	// release candidates without editing definitions; User works with what is
	// published (ADR-0047 §11).
	Builder       = "builder"
	Publisher     = "publisher"
	User          = "user"
	SchemaPublish = ObjectType + ".publish"
)

// Object is an object someone defines in this tenant: what it is called and
// which fields it holds. Publishing it installs it (ADR-0034 D2).
type Object struct {
	platform.Record
	Name        string  `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"visit"`
	Title       string  `json:"title" field:"required,search" title:"What people call it" example:"Visit"`
	Plural      string  `json:"plural,omitempty" title:"What people call several" example:"Visits"`
	Description string  `json:"description,omitempty" type:"longtext" help:"What one record of it is, for people and agents"`
	Fields      []Field `json:"fields" title:"Fields"`
	// VersionField names the integer field that numbers this object's versions
	// (ADR-0098 B5): a reference pinned to a version (Field.Pins) reads the
	// version current when the pin was taken; empty: the object is not versioned.
	VersionField string `json:"versionField,omitempty" field:"aside" title:"Version field" help:"The integer field that numbers this object's versions, e.g. version on a routing"`
	// States and Actions make its records a small process (ADR-0037): the
	// object editor owns them, so generated forms do not ask for them.
	States  []State  `json:"states,omitempty" field:"aside" title:"States"`
	Actions []Action `json:"actions,omitempty" field:"aside" title:"Actions"`
	// Access is who may do what with it (ADR-0037 18b); empty: builder and
	// user do everything, as before.
	Access []Access `json:"access,omitempty" field:"aside" title:"Access"`
	// Scope says which fields place a record for the row scopes of Access:
	// its owner (for "own"; empty: whoever created it) and its organisational
	// unit within a structure (for "unit" and "below") (ADR-0066).
	Scope *ObjectScope `json:"scope,omitempty" type:"json" field:"aside" title:"Scope fields"`
	// Implements names the interfaces this object carries the fields of; Extends
	// names an installed object type this one adds fields to, one record per base
	// record through its `base` reference (ADR-0058 A2, A3).
	Implements []string `json:"implements,omitempty" field:"aside" title:"Implements" help:"Interfaces whose fields this object has, e.g. core.coded"`
	Extends    string   `json:"extends,omitempty" field:"aside" title:"Extends" help:"The installed object type this object adds fields to, e.g. core.person; records pair one to one through the base field"`
	// Numbering (ADR-0076) gives every new record a document number in a text field: <prefix>[<year>-]<n>, gapless in order of creation.
	Numbering *Numbering `json:"numbering,omitempty" field:"aside" type:"json" title:"Document numbering"`
	State     string     `json:"state" field:"readonly" choices:"draft,published"`
	Installed string     `json:"installed,omitempty" field:"readonly" title:"Installed as" help:"The type records of it are stored under"`
	// Published is the definition as it was last published, which is what is
	// installed and what a restore installs again — not the draft beside it.
	Published string `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
}

// Field is one field of a defined object, as a person describes it.
type Field struct {
	Property *platform.AssetBinding `json:"property,omitempty" type:"json" field:"aside"`
	Name     string                 `json:"name" field:"required" help:"Lower-case letters and digits" example:"visited"`
	Title    string                 `json:"title" field:"required" title:"Label" example:"Visited on"`
	Type     string                 `json:"type" field:"required" choices:"text,longtext,integer,decimal,money,date,datetime,boolean,choice,reference,tags,lines" example:"date"`
	Choices  string                 `json:"choices,omitempty" help:"For a choice: the values, comma-separated" example:"open,done"`
	Columns  []LineColumn           `json:"columns,omitempty" title:"Line columns" help:"For lines: the columns of each embedded row"`
	Ref      string                 `json:"ref,omitempty" title:"Refers to" help:"For a reference: the object it points at" example:"crm.account"`
	// Inverse names the relation seen from the referenced record (ADR-0040 21b).
	Inverse string `json:"inverse,omitempty" title:"Seen from there as" help:"For a reference: what the referenced record calls these records" example:"visits"`
	// Pins, for a reference, names the integer field of this record that holds
	// the version of the referenced record this record is bound to (ADR-0098 B5).
	// Records that refer back through a pinned reference are read at that version.
	Pins     string `json:"pins,omitempty" title:"Pinned version field" help:"For a reference: the integer field of this record that holds the referenced record's version" example:"routingversion"`
	Required bool   `json:"required,omitempty"`
	Search   bool   `json:"search,omitempty" title:"Searchable"`
	// When bounds the field to another field's value (WorkQueue #138/#132):
	// "kind=refund,return" or "urgent=true". The field is asked for, and
	// required if marked so, only while the condition holds; otherwise it
	// stays empty. One level: the condition's field is unconditional.
	When string `json:"when,omitempty" title:"Only when" help:"field=value,value — another choice or boolean field of this object and the values that make this field apply" example:"kind=refund,return"`
	// Formula computes an integer or decimal field from the record's other
	// number fields at every decision; such a field is read-only (ADR-0064).
	Formula string `json:"formula,omitempty" help:"For a number: price * qty, (ordered - received), …" example:"price * qty"`
	// Read and Write, when set, are the roles that read and set it (ADR-0028 D3).
	Read  []string `json:"read,omitempty" title:"Read by" help:"Roles that read it; empty: every role that reads the object"`
	Write []string `json:"write,omitempty" title:"Set by" help:"Roles that set it; empty: every role that edits the object"`
}

// LineColumn is one column of an embedded lines field (ADR-0098 D1).
type LineColumn struct {
	Name     string `json:"name" field:"required" help:"Lower-case letters and digits" example:"target"`
	Title    string `json:"title" field:"required" title:"Label" example:"Target"`
	Type     string `json:"type" field:"required" choices:"text,integer,decimal,date,boolean,choice" example:"decimal"`
	Choices  string `json:"choices,omitempty" help:"For a choice column: the values, comma-separated"`
	Required bool   `json:"required,omitempty"`
}

// Installed reports the entity type an object is installed as.
func TypeOf(name string) string { return ID + "." + name }

func ValueChoices(s string) []string {
	var out []string
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}
