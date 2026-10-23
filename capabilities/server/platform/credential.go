package platform

import "time"

// CredentialBinding grants use by an original consumer at a specific network origin.
// It does not grant anyone permission to read the value.
type CredentialBinding struct {
	Consumer string `json:"consumer"`
	Origin   string `json:"origin"`
}
type CredentialDefinition struct {
	ID        string              `json:"id"`
	Title     string              `json:"title"`
	Kind      string              `json:"kind"`
	Source    string              `json:"source"`
	Reference string              `json:"reference,omitempty"`
	Bindings  []CredentialBinding `json:"bindings"`
}
type CredentialInfo struct {
	CredentialDefinition
	Version   int       `json:"version"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy string    `json:"updatedBy"`
}
type CredentialWrite struct {
	CredentialDefinition
	ExpectedVersion int    `json:"expectedVersion"`
	Sealed          string `json:"sealed,omitempty"`
}
type CredentialSealRequest struct {
	CredentialWrite
	Value string `json:"value"`
}
type CredentialCatalog struct {
	Credentials      []CredentialInfo `json:"credentials"`
	ManagedAvailable bool             `json:"managedAvailable"`
}
