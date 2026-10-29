package acceptedformat

import (
	"hash/fnv"
	"platformserver/platform"
	"slices"
	"strconv"
	"time"
)

const EffectRetryAttempts = 12

// effectRetry is how an effect tries again: 5 s … 1 h apart, about seven hours, then failed.
var effectRetry = platform.Retry{Initial: 5 * time.Second, Max: time.Hour, Attempts: EffectRetryAttempts}

// effectBackoff grows from 5 s to an hour, with jitter derived from the key so
// a replay computes the same due time.
func effectBackoff(id string, attempts int) time.Duration {
	d := effectRetry.After(attempts)
	h := fnv.New32a()
	h.Write([]byte(id + strconv.Itoa(attempts)))
	return d + time.Duration(h.Sum32()%1000)*d/5000 // up to +20 %
}

func MarkEffect(x *platform.Effect, since int, o platform.Outcome, at time.Time) {
	x.Last, x.Digest, x.Error, x.PublicMessage = at, o.Digest, o.Detail, o.PublicMessage
	if x.State == "discarded" {
		return // discarded while its attempt was on the way
	}
	if x.Endpoint == OperationEndpoint && o.Result == "delivered" {
		x.Output, x.Millis = slices.Clone(o.Answer), o.Millis
	}
	x.Attempts++
	switch {
	case o.Result == "delivered":
		x.State = "delivered"
	case o.Result == "rejected":
		x.State = "rejected"
	case o.Result == "unknown":
		x.State = "unknown"
	case x.Attempts-since >= effectRetry.Attempts:
		x.State = "failed"
	default:
		x.State, x.Due = "retrying", at.Add(effectBackoff(x.ID, x.Attempts-since))
	}
}
