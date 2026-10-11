package platformserver

// acceptResult is the storage boundary for every new authoritative result.
// Callers stage first and apply only the returned bytes. Record preserves its
// fatal-on-append-failure contract; memory has identical decision semantics
// without process-crash durability. The configured durable journal owns its
// ambiguous-commit lookup and idempotency checks.
func (t *Tenant) acceptResult(entry Entry, key, hash string) ([]byte, error) {
	if t.AcceptResult != nil {
		return t.AcceptResult(entry, key, hash)
	}
	if t.Record != nil {
		t.Record(entry)
	}
	return entry.Body, nil
}
