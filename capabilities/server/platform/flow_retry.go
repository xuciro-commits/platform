package platform

import "fmt"

// StreamPreparationRetry freezes the source/window preparation policy with
// its native definition. Zero attempts preserves the old single preparation.
// No cursor, action or authoritative state advances while preparation retries.
type StreamPreparationRetry struct {
	Attempts  int `json:"attempts"`
	BackoffMS int `json:"backoffMs"`
}

func (r *StreamPreparationRetry) Check() error {
	if r == nil {
		return nil
	}
	if r.Attempts < 0 || r.Attempts > 20 || r.BackoffMS < 0 || r.BackoffMS > 5000 || (r.Attempts-1)*r.BackoffMS > 30000 {
		return fmt.Errorf("stream preparation retries must fit 20 attempts and 30 seconds of bounded backoff")
	}
	return nil
}
func (r *StreamPreparationRetry) Tries() int {
	if r == nil || r.Attempts == 0 {
		return 1
	}
	return r.Attempts
}
