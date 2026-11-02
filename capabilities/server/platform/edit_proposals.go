package platform

const EditProposalVersion = "edit-proposal/v1"

type EditProposal struct {
	Kind  string           `json:"kind"`
	Edits []ProposedAction `json:"edits"`
}

type ProposedAction struct {
	Schema   string `json:"schema"`
	Type     string `json:"type"`
	ID       string `json:"id"`
	Payload  string `json:"payload"`
	Revision uint32 `json:"revision"`
}

func EditProposalSchema() ValueSchema {
	return ValueSchema{Type: "object", Required: []string{"kind", "edits"}, Properties: map[string]ValueSchema{
		"kind": {Type: "string", Enum: []string{EditProposalVersion}},
		"edits": {Type: "array", MinItems: 1, MaxItems: 64, Items: &ValueSchema{Type: "object", Required: []string{"schema", "type", "id", "payload", "revision"}, Properties: map[string]ValueSchema{
			"schema": {Type: "string"}, "type": {Type: "string"}, "id": {Type: "string"}, "payload": {Type: "string", Description: "The proposed action's JSON inputs"}, "revision": {Type: "integer"},
		}}},
	}}
}
