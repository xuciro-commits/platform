package journal

import "fmt"

type StatePositionError struct {
	Tenant            string
	Position, Applied int64
}

func (e *StatePositionError) Error() string {
	return fmt.Sprintf("tenant %s SQL state is at %d, applied at %d", e.Tenant, e.Position, e.Applied)
}
