package protocol

import (
	"fmt"

	"platformserver/platform"
)

func CheckStreamPreparationRetry(r *platform.StreamPreparationRetry) error {
	if r == nil {
		return nil
	}
	if r.Attempts < 0 || r.Attempts > 20 || r.BackoffMS < 0 || r.BackoffMS > 5000 || (r.Attempts-1)*r.BackoffMS > 30000 {
		return fmt.Errorf("stream preparation retries must fit 20 attempts and 30 seconds of bounded backoff")
	}
	return nil
}

func StreamPreparationRetryTries(r *platform.StreamPreparationRetry) int {
	if r == nil || r.Attempts == 0 {
		return 1
	}
	return r.Attempts
}
