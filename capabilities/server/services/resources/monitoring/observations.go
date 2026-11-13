package monitoring

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func OwnerRunObservation(at time.Time, source, runID, app, resource, status, title, detail string) platform.Observation {
	level, outcome := "info", "ok"
	switch status {
	case "failed":
		level, outcome = "error", "failed"
	case "cancelled":
		level, outcome = "warn", "refused"
	case "pending", "running":
		level, outcome = "info", "pending"
	}
	attributes := map[string]string{"status": status}
	if detail != "" {
		if len(detail) > 300 {
			detail = detail[:300]
		}
		attributes["error"] = detail
	}
	return platform.Observation{
		At: at, Level: level, Source: source, App: app, RunID: runID,
		Resource: resource, Outcome: outcome, Message: title + " " + status, Attributes: attributes,
	}
}

func RequestObservation(at time.Time, member, route, requestID, traceID, spanID string, status int, duration time.Duration) platform.Observation {
	level, outcome := "info", "ok"
	switch {
	case status >= http.StatusInternalServerError:
		level, outcome = "error", "failed"
	case status >= http.StatusBadRequest:
		level, outcome = "warn", "refused"
	}
	return platform.Observation{
		At:        at,
		Level:     level,
		Source:    "request",
		Member:    member,
		Route:     route,
		RequestID: requestID,
		TraceID:   traceID,
		SpanID:    spanID,
		Outcome:   outcome,
		Message:   fmt.Sprintf("Request completed with status %d", status),
		Attributes: map[string]string{
			"status":     strconv.Itoa(status),
			"durationMs": strconv.FormatInt(duration.Milliseconds(), 10),
		},
	}
}

func SubmissionObservation(at time.Time, member, requestID, traceID, spanID string, sub *pb.Submission, record *pb.ChangeRecord, refusal *kernel.Error) platform.Observation {
	level, outcome := "info", "ok"
	if refusal != nil {
		level, outcome = "warn", "refused"
	}
	attributes := map[string]string{}
	if schema := sub.GetSchema().GetName(); schema != "" {
		attributes["schema"] = schema
	}
	if refusal != nil {
		attributes["errorCode"] = refusal.Error()
	}
	event := platform.Observation{
		At:         at,
		Level:      level,
		Source:     "submit",
		Member:     member,
		App:        sub.GetAuthority(),
		Route:      "POST /v1/submissions",
		RequestID:  requestID,
		TraceID:    traceID,
		SpanID:     spanID,
		Outcome:    outcome,
		Message:    "Submission accepted",
		Attributes: attributes,
	}
	if refusal != nil {
		event.Message = "Submission refused"
	}
	if record != nil {
		event.ChangeID = record.GetChangeId()
	}
	return event
}
