// Package knowledge is the platform's knowledge app (ADR-0022), on the app API
// and internal/host (ADR-0025 D4): documents people upload for agents and
// members to find, and the tenant's glossary. The host searches them, with
// the fields apps declare as knowledge (knowledge.go in the host).
package knowledge

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

const (
	ID                    = "knowledge"
	DocumentType          = "knowledge.document"
	TermType              = "knowledge.term"
	Editor                = "editor"
	SettingEmbeddingModel = "embedding-model"
)

// Document is a text people upload for agents and members to find.
type Document struct {
	platform.Record
	Title  string   `json:"title" field:"required,search"`
	Text   string   `json:"text" field:"required" type:"longtext"`
	Source string   `json:"source,omitempty" title:"Where it comes from"`
	Apps   []string `json:"apps,omitempty" title:"Read by members of"` // the apps whose members may read it; none: every member
}

// Term is a word of the tenant's own glossary (ADR-0023 D1): what it means
// here, other words for it, and the declaration it refers to. It is layered on
// top of the model: agents read it and search expands by it, but it never
// renames, retitles or redefines a declaration.
type Term struct {
	platform.Record
	Term     string   `json:"term" field:"required,search" help:"The word people here use" example:"PO"`
	Meaning  string   `json:"meaning" field:"required" type:"longtext" help:"What it means in this organisation"`
	Synonyms string   `json:"synonyms,omitempty" help:"Other words for it, comma-separated"`
	RefersTo string   `json:"refersTo,omitempty" title:"Refers to" help:"The declaration it names: an entity type, <type>.<field> or an action" example:"mes.order"`
	Apps     []string `json:"apps,omitempty" title:"Read by members of"`
}

type Knowledge struct {
	host   host.Host // the tenant running it, once composed
	ledger *platform.Ledger
}

// Attach is called by the host when a tenant is composed.
func (k *Knowledge) Attach(h host.Host) { k.host = h }

// New is a tenant's knowledge app.
func New(tenant string) *Knowledge {
	k := &Knowledge{}
	es := knowledgeEntities()
	k.ledger = platform.NewLedger(tenant, ID, platform.NewCatalog(append(platform.EntityActions(es[0]), platform.EntityActions(es[1])...)...), DocumentType, TermType)
	return k
}

func knowledgeEntities() []platform.Entity {
	return []platform.Entity{{Type: DocumentType, Title: "Document", Model: Document{}, Display: "title",
		Description: "A text people upload for agents and members to find and cite: house rules, manuals, FAQs, contracts.",
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Editor}, Capability: "documents"}},
		{Type: TermType, Title: "Term", Model: Term{}, Display: "term", Synonyms: "glossary",
			Description: "A word of this organisation's own glossary, layered on the platform's model: agents read it and search understands it; it never changes what a declaration is.",
			Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Editor}, Capability: "glossary"}}}
}

func (k *Knowledge) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: "Knowledge", Version: "1", Actions: k.ledger.Catalog, Entities: knowledgeEntities(),
		Settings: []platform.Setting{{Name: SettingEmbeddingModel, Title: "Embedding model", Type: "text", Default: "",
			Description: "The enabled model that embeds passages, <provider>/<model>, on the OpenAI wire. Empty: knowledge is searched by words only."}}}
}

func (k *Knowledge) Declarations() []*pb.AuthorityDeclaration { return k.ledger.Declarations() }
func (k *Knowledge) Snapshot() (json.RawMessage, error)       { return k.ledger.Snapshot() }
func (k *Knowledge) Restore(raw json.RawMessage) error        { return k.ledger.Restore(raw) }
func (k *Knowledge) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
func (k *Knowledge) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (k *Knowledge) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if name := s.GetSchema().GetName(); (name == TermType+".create" || name == TermType+".edit") && !c.Replaying {
		var p struct{ RefersTo *string }
		json.Unmarshal(s.GetPayload(), &p)
		if p.RefersTo != nil && *p.RefersTo != "" && !k.host.Declares(*p.RefersTo) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT} // a term names what exists; it cannot make a declaration
		}
	}
	if record, err, ok := k.ledger.Generated(c, s, now, nil, knowledgeEntities()...); ok {
		return record, err
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// Terms are the glossary terms an app's members may read ("" for every term).
func (k *Knowledge) Terms(app string) []Term {
	all, _, _ := platform.Find[Term](k.host.Automation(ID, false), platform.Query{Limit: 500, Sort: []string{"term"}})
	out := all[:0]
	for _, x := range all {
		if app == "" || len(x.Apps) == 0 || slices.Contains(x.Apps, app) {
			out = append(out, x)
		}
	}
	return out
}

// TermsFor are the words the glossary gives a declaration: each term that
// refers to it, and its synonyms.
func (k *Knowledge) TermsFor(declaration string) []string {
	var out []string
	for _, x := range k.Terms("") {
		if x.RefersTo == declaration {
			out = append(out, x.Term)
			out = append(out, strings.Split(x.Synonyms, ",")...)
		}
	}
	return out
}
