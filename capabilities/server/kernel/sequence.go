package kernel

import (
	"sync/atomic"
	"time"

	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
)

type Sequence struct {
	ID               string
	Store            persistence.Store
	memory           persistence.Memory
	Lane             Lane
	LastSlowHold     atomic.Pointer[LaneHold]
	Committed        Committed
	Projection       *records.Store
	Fault            atomic.Pointer[Fault]
	StateEpoch       atomic.Uint64
	WorkEpoch        atomic.Uint64
	DefinitionEpoch  atomic.Uint64
	SnapshotPosition int64
	RuntimePosition  func() int64
	Record           func(persistence.Entry)
	AcceptResult     func(persistence.Entry, string, string) ([]byte, error)
}

type LaneHold struct {
	Holder  string    `json:"holder"`
	Seconds float64   `json:"seconds"`
	At      time.Time `json:"at"`
}

type Fault struct {
	Reason string
}

func (s *Sequence) Log() persistence.Store {
	if s.Store != nil {
		return s.Store
	}
	return &s.memory
}
