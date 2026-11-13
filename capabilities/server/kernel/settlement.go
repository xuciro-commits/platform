package kernel

import "time"

type Settlement struct {
	Quarantine        func(error)
	Applied           func()
	Enqueue           func(time.Time)
	ChangedOwner      func(app string)
	OperationsChanged func(app ...string)
}

func (s Settlement) Settled(at time.Time, app string, changed bool) {
	s.Applied()
	s.Enqueue(at)
	if changed {
		s.ChangedOwner(app)
		return
	}
	s.OperationsChanged(app)
}
