package platform

import (
	"encoding/json"
	"time"
)

type Versioning struct {
	Key        string `json:"key"`
	Version    string `json:"version"`
	ValidFrom  string `json:"validFrom"`
	ValidUntil string `json:"validUntil,omitempty"`
	Pin        string `json:"pin"`
}

type VersionPinBinding struct {
	Reference string `json:"reference"`
	Version   string `json:"version"`
}

type RecordVersionPin struct {
	Reference    string          `json:"reference"`
	VersionField string          `json:"versionField"`
	Source       string          `json:"source"`
	Version      json.RawMessage `json:"version"`
	At           time.Time       `json:"at"`
}

type BusinessTime struct {
	At  string `json:"at,omitempty"`
	All bool   `json:"all,omitempty"`
}

func (c Caller) PrepareBusinessVersions(record any, at time.Time) *Error {
	if c.Replaying {
		return nil
	}
	if r := c.StagedDecision(); r != nil {
		return r.PrepareBusinessVersions(c, record, at)
	}
	return nil
}
