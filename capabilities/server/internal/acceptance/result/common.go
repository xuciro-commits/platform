// Package result owns immutable accepted-result formats and their pure checks.
// Persistence, private decisions and authoritative application stay with owners.
package result

import (
	"encoding/json"
	"platformserver/internal/construction"
	"platformserver/internal/records"
	"platformserver/platform"
	"time"
)

type Event struct {
	App         string            `json:"app"`
	Changed     []string          `json:"changed"`
	Names       []string          `json:"names,omitempty"`
	Subscribers []string          `json:"subscribers,omitempty"`
	Effects     []platform.Effect `json:"effects,omitempty"`
	Observed    bool              `json:"observed,omitempty"`
	Hops        int               `json:"hops,omitempty"`
	Stopped     []string          `json:"stopped,omitempty"`
}

type State struct {
	App    string          `json:"app"`
	Before string          `json:"before"`
	Image  json.RawMessage `json:"image"`
}

// Notices freezes rendered notifications and mail intents before enqueue.
type Notices struct {
	Base    int                     `json:"base"`
	Seq     int                     `json:"seq"`
	Before  string                  `json:"before"`
	Notices []platform.Notification `json:"notices"`
	Effects []platform.Effect       `json:"effects,omitempty"`
}

// Control carries each owner's immutable image and predecessor separately.
// Runtime tasks and control application remain with their original owners.
type Control struct {
	Changes []ControlChange `json:"changes"`
}

type ControlChange struct {
	Kind   string          `json:"kind"`
	Before string          `json:"before"`
	Image  json.RawMessage `json:"image"`
}

// ConnectorDelivery guards the connector cursor and last-seen mark together.
type ConnectorDelivery struct {
	Connector string    `json:"connector"`
	DataClass string    `json:"dataClass"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	At        time.Time `json:"at"`
	Before    string    `json:"before"`
}

type Observation struct {
	App  string          `json:"app"`
	Body json.RawMessage `json:"body"`
}

type OperationCancellation struct {
	Before    platform.Effect `json:"before"`
	After     platform.Effect `json:"after"`
	PriorWork json.RawMessage `json:"priorWork,omitempty"`
	Work      json.RawMessage `json:"work,omitempty"`
}

type ReleaseInstallation struct {
	construction.ReleasePublication
	Before string             `json:"before"`
	Ref    *platform.AssetRef `json:"ref,omitempty"`
	Record string             `json:"record,omitempty"`
}

type Publication struct {
	Format string          `json:"format,omitempty"`
	Schema string          `json:"schema"`
	Image  json.RawMessage `json:"image"`
}

type Decision struct {
	Definition  *DecisionDefinition `json:"definition,omitempty"`
	Receipt     json.RawMessage     `json:"receipt"`
	Event       Event               `json:"event"`
	Publication *Publication        `json:"publication,omitempty"`
}

type BatchRow struct {
	records.Image
	App         string   `json:"app"`
	Before      string   `json:"before,omitempty"`
	Authorities []string `json:"authorities,omitempty"` // receipt authority for each new history entry
	HistoryBase int      `json:"historyBase,omitempty"` // format 2: retained prefix length; History carries only this input's suffix
}

type InputRow struct {
	records.Image
	App    string `json:"app"`
	Before string `json:"before,omitempty"`
}

type CallbackRow struct {
	Row    InputRow        `json:"row"`
	Source platform.Effect `json:"source"`
}
