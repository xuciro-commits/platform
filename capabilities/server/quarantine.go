package platformserver

import "fmt"

// tenantFault records the first unrecoverable tenant-local failure. A restart
// after repairing the log or restoring a valid backup is the only way to clear
// it; the partially applied in-memory tenant is never resumed.
type tenantFault struct {
	Reason string
}

func (t *Tenant) quarantine(err error) {
	if err == nil {
		return
	}
	t.fault.CompareAndSwap(nil, &tenantFault{Reason: fmt.Sprintf("%v", err)})
}

func (t *Tenant) quarantined() bool { return t.fault.Load() != nil }

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
