package sources

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	actioncatalog "platformserver/services/actions/catalog"
)

const (
	ID                    = "knowledge"
	DocumentType          = "knowledge.document"
	TermType              = "knowledge.term"
	Editor                = "editor"
	SettingEmbeddingModel = "embedding-model"
)

type Document struct {
	platform.Record
	Title  string   `json:"title" field:"required,search"`
	Text   string   `json:"text" field:"required" type:"longtext"`
	Source string   `json:"source,omitempty" title:"Source" help:"Where it comes from"`
	Apps   []string `json:"apps,omitempty" title:"Reader apps" help:"Read by members of"`
}

type Term = platform.KnowledgeTerm

type Knowledge struct {
	host      platform.KnowledgeHost
	authority platform.Authority
}

func (k *Knowledge) AttachKnowledge(h platform.KnowledgeHost) { k.host = h }

func (k *Knowledge) AttachOwner(bindings runtimeprotocol.OwnerBindings) {
	k.AttachKnowledge(bindings.Knowledge)
}

func New(tenant string, authorities platform.Authorities) *Knowledge {
	k := &Knowledge{}
	es := knowledgeEntities()
	k.authority = authorities(tenant, ID, platform.NewCatalog(append(actioncatalog.EntityActions(es[0]), actioncatalog.EntityActions(es[1])...)...), DocumentType, TermType)
	return k
}

func knowledgeEntities() []platform.RecordType {
	return []platform.RecordType{{Type: DocumentType, Title: "Document", Model: Document{}, Display: "title",
		Description: "A text people upload for agents and members to find and cite: house rules, manuals, FAQs, contracts.",
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Editor}, Capability: "documents"}},
		{Type: TermType, Title: "Term", Model: Term{}, Display: "term", Synonyms: "glossary",
			Description: "A word of this organisation's own glossary, layered on the platform's model: agents read it and search understands it; it never changes what a declaration is.",
			Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Editor}, Capability: "glossary"}}}
}

func (k *Knowledge) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: platform.BuiltinManifestTitle(ID), Version: "1", Actions: k.authority.Catalog(), Entities: knowledgeEntities(),
		Settings: []platform.Setting{{Name: SettingEmbeddingModel, Title: "Embedding model", Type: "text", Default: "",
			Description: "The enabled model that embeds passages, <provider>/<model>, on the OpenAI wire. Empty: knowledge is searched by words only."},
			{Name: runtimeprotocol.SettingKnowledgeEmbeddingNext, Title: "Build vector index with", Type: "text", Default: "", Description: "Explicitly prepare an index with this model before activating it in AI configuration; indexing may incur provider charges."}}}
}

func (k *Knowledge) Authority() platform.Authority { return k.authority }

func (k *Knowledge) Declarations() []*pb.AuthorityDeclaration { return k.authority.Declarations() }
func (k *Knowledge) SnapshotFormat() string                   { return "platform.ledger.v1" }

func (k *Knowledge) Snapshot() (json.RawMessage, error) { return k.authority.Snapshot() }
func (k *Knowledge) Restore(raw json.RawMessage) error  { return k.authority.Restore(raw) }
func (k *Knowledge) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	if name := s.GetSchema().GetName(); (name == TermType+".create" || name == TermType+".edit") && !c.Replaying {
		var p struct{ RefersTo *string }
		json.Unmarshal(s.GetPayload(), &p)
		if p.RefersTo != nil && *p.RefersTo != "" && !k.host.Declares(*p.RefersTo) {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
	}
	if record, err, ok := k.authority.Generated(c, s, now, nil, knowledgeEntities()...); ok {
		return record, err
	}
	return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (k *Knowledge) Terms(app string) []Term {
	all, _, _ := platform.Find[Term](k.host.Automation(platform.Caller{}, ID), platform.Query{Limit: 500, Sort: []string{"term"}})
	out := all[:0]
	for _, x := range all {
		if app == "" || len(x.Apps) == 0 || slices.Contains(x.Apps, app) {
			out = append(out, x)
		}
	}
	return out
}

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

func (k *Knowledge) Fork(authority platform.Authority) (platform.Service, error) {
	return &Knowledge{host: k.host, authority: authority}, nil
}
