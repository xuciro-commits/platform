package result

import (
	"encoding/json"
	"platformserver/journal"
	"platformserver/platform"
	"time"
)

// ChangePlan projects immutable result data onto the one typed host application path.
// It does not own persistence, tenant locking or owner validation.
type ChangePlan struct {
	Rows          []InputRow
	Member        string
	At            time.Time
	Changes       json.RawMessage
	States        []State
	Control       *Control
	Intents       []platform.Effect
	Deliveries    []ConnectorDelivery
	Notices       *Notices
	Sequences     map[string]int
	SequenceBases map[string]int
	Unique        []journal.StateUnique
	CheckUnique   bool
}

func (r Input) ChangePlan() ChangePlan {
	return ChangePlan{Rows: r.Rows, Member: r.Member, At: r.At, Changes: r.Changes,
		States: r.States, Deliveries: r.Deliveries, Notices: r.Notices, Unique: r.Unique, CheckUnique: r.Version >= 2 && len(r.Changes) == 0}
}

func (r Effect) ChangePlan() ChangePlan {
	return ChangePlan{Rows: r.Rows, Member: r.Member, At: r.At, Changes: r.Changes,
		States: r.States, Control: r.Control, Intents: r.Intents, Notices: r.Notices, Unique: r.Unique, CheckUnique: r.Version >= 4}
}

func (r Batch) ChangePlan() ChangePlan {
	return ChangePlan{At: r.At, States: r.States, Control: r.Control, Intents: r.Intents,
		Deliveries: r.Deliveries, Notices: r.Notices, Sequences: r.Sequences,
		SequenceBases: r.SequenceBases, Unique: r.Unique, CheckUnique: r.Version >= 8}
}
