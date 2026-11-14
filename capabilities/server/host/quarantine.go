package host

import (
	"fmt"

	runtimekernel "platformserver/kernel"
)

func (t *Tenant) quarantine(err error) {
	if err == nil {
		return
	}
	if source := t.readOrigin; source != nil {
		source.quarantine(err)
		return
	}
	if t.Fault.CompareAndSwap(nil, &runtimekernel.Fault{Reason: fmt.Sprintf("%v", err)}) {
		t.changed()
	}
}

func (t *Tenant) quarantined() bool {
	if source := t.readOrigin; source != nil {
		return source.quarantined()
	}
	return t.Fault.Load() != nil
}

func (t *Tenant) guarded(stage string, run func() error) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("%s panicked: %v", stage, failure)
			t.quarantine(err)
		}
	}()
	if err := run(); err != nil {
		t.quarantine(fmt.Errorf("%s: %w", stage, err))
		return err
	}
	return nil
}

func (t *Tenant) recoverSnapshot(state []byte, seq int64) error {
	return t.guarded(fmt.Sprintf("restore snapshot %d", seq), func() error { return t.Restore(state) })
}

func (t *Tenant) recoverEntries(entries []Entry) error {
	return t.guarded("replay", func() error { return t.Replay(entries) })
}
