package host

import (
	"fmt"

	runtimekernel "platformserver/kernel"
)

func (t *Tenant) quarantine(err error) {
	if err == nil {
		return
	}
	if source := t.scopeSource(); source != nil {
		source.quarantine(err)
		return
	}
	if t.Fault.CompareAndSwap(nil, &runtimekernel.Fault{Reason: fmt.Sprintf("%v", err)}) {
		t.changed()
	}
}

func (t *Tenant) quarantined() bool {
	if source := t.scopeSource(); source != nil {
		return source.quarantined()
	}
	return t.Fault.Load() != nil
}

func (t *Tenant) recoverSnapshot(state []byte, seq int64) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("restore snapshot %d panicked: %v", seq, failure)
			t.quarantine(err)
		}
	}()
	if err := t.Restore(state); err != nil {
		t.quarantine(fmt.Errorf("restore snapshot %d: %w", seq, err))
		return err
	}
	return nil
}

func (t *Tenant) recoverEntries(entries []Entry) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("replay panicked: %v", failure)
			t.quarantine(err)
		}
	}()
	if err := t.Replay(entries); err != nil {
		t.quarantine(fmt.Errorf("replay: %w", err))
		return err
	}
	return nil
}
