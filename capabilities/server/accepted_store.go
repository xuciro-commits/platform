package platformserver

// acceptResult is the storage boundary for every new authoritative result.
// Callers stage first and apply only the returned bytes. Record preserves its
// fatal-on-append-failure contract; memory has identical decision semantics
// without process-crash durability. The configured durable journal owns its
// ambiguous-commit lookup and idempotency checks.
func (t *Tenant) acceptResult(entry Entry, key, hash string) ([]byte, error) {
	if t.AcceptResult != nil {
		before := int64(0)
		if t.runtimePosition != nil {
			before = t.runtimePosition()
			t.committed.releasePending()
		}
		raw, err := t.AcceptResult(entry, key, hash)
		if err == nil && t.runtimePosition != nil && t.runtimePosition() > before {
			delta, decodeErr := acceptedStorageDelta(raw)
			if decodeErr != nil {
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
