package platformserver

import (
	"errors"
	"fmt"

	"platformserver/journal"
)

// acceptResult is the storage boundary for every new authoritative result.
// Callers stage first and apply only the returned bytes. Record is the legacy
// local recording callback; production journals use AcceptResult to report
// failures. The durable journal resolves ambiguous commits under the original
// identity; an unresolved commit stops only this tenant's generation.
func (t *Tenant) acceptResult(entry Entry, key, hash string) ([]byte, error) {
	if t.AcceptResult != nil {
		before := int64(0)
		if t.runtimePosition != nil {
			before = t.runtimePosition()
			t.committed.releasePending()
		}
		raw, err := t.AcceptResult(entry, key, hash)
		if err != nil && (errors.Is(err, journal.ErrCommitUnknown) ||
			t.runtimePosition != nil && t.runtimePosition() > before) {
			t.quarantine(fmt.Errorf("accepted result commit could not be resolved: %w", err))
		}
		if err == nil && t.runtimePosition != nil && t.runtimePosition() > before {
			delta, decodeErr := acceptedStorageDelta(raw)
			if decodeErr != nil {
				t.quarantine(fmt.Errorf("committed state delta: %w", decodeErr))
				return nil, decodeErr
			}
			t.committed.holdPending(delta)
		}
		return raw, err
	}
	if t.Record != nil {
		t.Record(entry)
	}
	return entry.Body, nil
}

func (t *Tenant) recordsAcceptedResults() bool { return t.AcceptResult != nil || t.Record != nil }
