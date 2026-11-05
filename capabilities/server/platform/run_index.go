package platform

import (
	"encoding/json"
	"time"
)

type RunIndexEntry struct {
	ID      string          `json:"id"`
	Scope   string          `json:"scope,omitempty"`
	Type    string          `json:"type,omitempty"`
	Source  string          `json:"source,omitempty"`
	App     string          `json:"app,omitempty"`
	Status  string          `json:"status,omitempty"`
	At      time.Time       `json:"at,omitzero"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type RunIndexQuery struct {
	Tenant string
	Scope  string
	Type   string
	Source string
	App    string
	Status string
	From   time.Time
	To     time.Time
	Offset int
	Limit  int
}

type RunIndexPage struct {
	Entries      []RunIndexEntry `json:"entries"`
	Total        int             `json:"total"`
	RetainedFrom time.Time       `json:"retainedFrom,omitzero"`
}
