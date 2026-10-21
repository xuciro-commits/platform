package journal

import "fmt"

// StatePositionError means SQL has advanced beyond (or differs from) the
// owner's applied state. A new read cannot reconstruct that missing view by
// excluding newer rows; the owner must recover before serving again.
type StatePositionError struct {
	Tenant            string
	Position, Applied int64
}

func (e *StatePositionError) Error() string {
	return fmt.Sprintf("tenant %s SQL state is at %d, applied at %d", e.Tenant, e.Position, e.Applied)
}
