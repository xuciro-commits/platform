package platform

const (
	KnowledgeApp                   = "knowledge"
	KnowledgeDocumentType          = "knowledge.document"
	KnowledgeTermType              = "knowledge.term"
	SettingKnowledgeEmbeddingModel = "embedding-model"
)

// Document is a text people upload for agents and members to find.
type KnowledgeDocument struct {
	Record
	Title  string   `json:"title" field:"required,search"`
	Text   string   `json:"text" field:"required" type:"longtext"`
	Source string   `json:"source,omitempty" title:"Where it comes from"`
	Apps   []string `json:"apps,omitempty" title:"Read by members of"` // the apps whose members may read it; none: every member
}

// Term is a word of the tenant's own glossary (ADR-0023 D1): what it means
// here, other words for it, and the declaration it refers to. It is layered on
// top of the model: agents read it and search expands by it, but it never
// renames, retitles or redefines a declaration.
type KnowledgeTerm struct {
	Record
	Term     string   `json:"term" field:"required,search" help:"The word people here use" example:"PO"`
	Meaning  string   `json:"meaning" field:"required" type:"longtext" help:"What it means in this organisation"`
	Synonyms string   `json:"synonyms,omitempty" help:"Other words for it, comma-separated"`
	RefersTo string   `json:"refersTo,omitempty" title:"Refers to" help:"The declaration it names: an entity type, <type>.<field> or an action" example:"mes.order"`
	Apps     []string `json:"apps,omitempty" title:"Read by members of"`
}
