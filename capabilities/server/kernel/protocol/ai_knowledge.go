package protocol

import ()

const (
	KnowledgeApp                   = "knowledge"
	KnowledgeDocumentType          = "knowledge.document"
	SettingKnowledgeEmbeddingModel = "embedding-model"
	SettingKnowledgeEmbeddingNext  = "embedding-next-model"
)

type Passage struct {
	Document string  `json:"document"`
	Title    string  `json:"title"`
	Chunk    int     `json:"chunk"`
	Text     string  `json:"text"`
	Score    float64 `json:"score"`
}

type KnowledgeIndexStatus struct {
	NeedsRebuild bool   `json:"needsRebuild,omitempty"`
	Active       string `json:"active"`
	Preparing    string `json:"preparing"`
	Total        int    `json:"total"`
	Completed    int    `json:"completed"`
	Dimensions   int    `json:"dimensions,omitempty"`
	Ready        bool   `json:"ready"`
	Problem      string `json:"problem,omitempty"`
}
