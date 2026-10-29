package acceptedformat

import (
	"hash/fnv"
	"platformserver/platform"
	"slices"
	"strconv"
	"time"
)

const EffectRetryAttempts = 12

var effectRetry = platform.Retry{Initial: 5 * time.Second, Max: time.Hour, Attempts: EffectRetryAttempts}

func effectBackoff(id string, attempts int) time.Duration {
	d := effectRetry.After(attempts)
	h := fnv.New32a()
	h.Write([]byte(id + strconv.Itoa(attempts)))
	return d + time.Duration(h.Sum32()%1000)*d/5000
}

func MarkEffect(x *platform.Effect, since int, o platform.Outcome, at time.Time) {
	x.Last, x.Digest, x.Error, x.PublicMessage = at, o.Digest, o.Detail, o.PublicMessage
	if x.State == "discarded" {
		return
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
