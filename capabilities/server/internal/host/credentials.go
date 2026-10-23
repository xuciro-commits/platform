package host

import "platformserver/platform"

// Credentials is the original governance owner's encrypted state, read without its directory lock.
type Credentials interface {
	Credential(string) (CredentialState, bool)
}
type CredentialState struct {
	platform.CredentialInfo
	SealedVersion int    `json:"sealedVersion,omitempty"`
	Sealed        string `json:"sealed,omitempty"`
}
