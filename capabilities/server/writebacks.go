package platformserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"platformserver/internal/host"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

// IntegrationEffect exposes delivery metadata, without requests or answers.
type IntegrationEffect struct {
	ID       string    `json:"id"`
	Endpoint string    `json:"endpoint"`
	Event    string    `json:"event"`
	State    string    `json:"state"`
	Due      time.Time `json:"due"`
	Last     time.Time `json:"last,omitzero"`
	Error    string    `json:"error,omitempty"`
}

func (t *Tenant) integrationEffects(m platform.Member, now time.Time) ([]IntegrationEffect, *kernel.Error) {
	if err := t.admits(m); err != nil {
		return nil, err
	}
	if !m.Holds(construction.ID, construction.Builder) && !m.Holds(construction.ID, construction.Integrator) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	out := []IntegrationEffect{}
	for _, effect := range t.Effects(now) {
		writeback := strings.HasPrefix(effect.Event, "writeback/") && strings.HasPrefix(effect.Endpoint, construction.WritebackEndpoint)
		books := (strings.HasPrefix(effect.Event, "journal/") || effect.Event == construction.ID+"/journal") && effect.Endpoint == construction.BooksEndpoint
		confirmation := effect.Event == construction.ID+"/confirmation" && strings.HasPrefix(effect.Endpoint, construction.WritebackEndpoint)
		dispatch := effect.Event == construction.ID+"/dispatch" && strings.HasPrefix(effect.Endpoint, construction.WritebackEndpoint)
		transaction := effect.Event == construction.ID+"/transaction" && strings.HasPrefix(effect.Endpoint, construction.WritebackEndpoint)
		if effect.App != construction.ID || !writeback && !books && !confirmation && !dispatch && !transaction {
			continue
		}
		item := IntegrationEffect{ID: effect.ID, Endpoint: effect.Endpoint, Event: effect.Event, State: effect.State, Due: effect.Due, Last: effect.Last}
		if effect.Error != "" {
			item.Error = "The last delivery attempt failed"
			if books && (strings.Contains(effect.Error, "closed") || strings.Contains(effect.Error, "fiscal period")) {
				item.Error = "Waiting for an open fiscal period"
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// Writebacks (ADR-0024) ride the ADR-0004 outbox: an accepted decision that a
// published writeback listens for becomes an effect addressed to the
// connection ("connection:<id>"), frozen with its request while the decision is
// still private, so replay rebuilds the same intent. The dispatcher sends it in
// the connection's order with the decision's change id as the idempotency key;
// its outcome is journaled, and the build app hears the answer (Answerer).

// writebackEffects plans the effects of one accepted decision. It runs inside
// the input and inside replay, so it only reads records as they stand.
func (t *Tenant) writebackEffects(e platform.Event) []platform.Effect {
	if t.app(construction.ID) == nil {
		return nil
	}
	s := e.Record.GetSubmission()
	schema := s.GetSchema().GetName()
	if e.App != construction.ID {
		return nil
	}
	c := t.automation(construction.ID, false)
	writebacks, _, _ := platform.Find[construction.Writeback](c, platform.Query{Domain: json.RawMessage(`[["state","=","published"]]`), Sort: []string{"id"}, Limit: 200})
	var planned []platform.Effect
	owner := t.app(construction.ID).(host.IntegrationDefinitions)
	for _, w := range writebacks {
		w, err := owner.ExecutionWriteback(w)
		if err != nil {
			continue
		}
		if w.On == "confirmation" || w.On == "dispatch" || w.On == "transaction" || w.Schema() != schema {
			continue
		}
		conn, ok := platform.Get[construction.Connection](c, w.Connection)
		if !ok {
			continue
		}
		conn, err = owner.ExecutionConnection(conn)
		if err != nil {
			continue
		}
		req, err := w.Request(conn, s.GetTarget().GetId(), s.GetPayload())
		if err != nil {
			continue
		}
		body, _ := json.Marshal(req)
		at := e.Record.GetRecordedTime().AsTime()
		planned = append(planned, platform.Effect{ID: fmt.Sprintf("%s:%s:%s:%s", t.ID, construction.ID, e.Record.GetChangeId(), w.ID),
			Endpoint: construction.WritebackEndpoint + conn.ID, Event: "writeback/" + w.Name, App: construction.ID, Key: e.Record.GetChangeId(), Target: target(s), At: at,
			State: "pending", Due: at, Body: string(body)})
	}
	return planned
}

// connectionEndpoints are the connections with unsettled writebacks, as the
// dispatcher's endpoints: one order and one breaker per connection. Called
// under opsMu.
func (t *Tenant) connectionEndpoints() []*Endpoint {
	var out []*Endpoint
	for _, x := range t.outbound {
		if !strings.HasPrefix(x.Endpoint, construction.WritebackEndpoint) || settled(x.State) {
			continue
		}
		if !slices.ContainsFunc(out, func(ep *Endpoint) bool { return ep.ID == x.Endpoint }) {
			out = append(out, &Endpoint{ID: x.Endpoint, Kind: "connection"})
		}
	}
	return out
}

// sendWriteback makes one attempt with the frozen request: the connection's
// secret as Authorization, the effect id as Idempotency-Key.
func (t *Tenant) sendWriteback(x platform.Effect) (out platform.Outcome) {
	sum := sha256.Sum256([]byte(x.Body))
	out = platform.Outcome{Effect: x.ID, Digest: hex.EncodeToString(sum[:])}
	var req construction.WritebackRequest
	if json.Unmarshal([]byte(x.Body), &req) != nil {
		out.Result, out.Detail = "rejected", "the request is unreadable"
		return out
	}
	defer func() {
		if req.Dispatch != "" && !req.SafeRetry && out.Result == "retry" {
			out.Result, out.Detail = "unknown", "This command cannot be resent automatically: "+out.Detail
		}
	}()
	u, err := url.Parse(req.URL)
	if err != nil || u.Scheme != "https" && !(u.Scheme == "http" && req.AllowPrivate) {
		out.Result, out.Detail = "rejected", "only https addresses (http when private addresses are allowed)"
		return out
	}
	var requestBody any = req.Body
	if req.Protocol != nil {
		if req.Dispatch == "" || req.DriverDispatch == nil || req.DriverCommand == nil {
			out.Result, out.Detail = "rejected", "Protocol command has no original dispatch identity"
			return out
		}
		connection := construction.Connection{Record: platform.Record{ID: req.Connection}, Kind: "http", Address: req.Gateway, Secret: req.Secret, AllowPrivate: req.AllowPrivate, Protocol: req.Protocol}
		if err := t.configureProtocol(context.Background(), connection, true); err != nil {
			out.Result, out.Detail = "unknown", err.Error()
			return out
		}
		requestBody = platform.DriverExecute{Dispatch: *req.DriverDispatch, Command: *req.DriverCommand}
	}
	body, _ := json.Marshal(requestBody)
	r, err := http.NewRequest(req.Method, req.URL, strings.NewReader(string(body)))
	if err != nil {
		out.Result, out.Detail = "rejected", "the request method or address is invalid"
		return out
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Idempotency-Key", x.ID)
	if req.Dispatch != "" {
		r.Header.Set("Idempotency-Key", req.DispatchKey)
	}
	if req.Secret != "" {
		v, ok := t.secret(req.Secret)
		if !ok {
			out.Result, out.Detail = "retry", "secret "+req.Secret+" missing"
			return out
		}
		r.Header.Set("Authorization", strings.TrimSpace(string(v)))
	}
	send := t.Outbound
	if send == nil {
		send = guarded
	}
	resp, err := send(r, req.AllowPrivate)
	switch {
	case err != nil && strings.Contains(err.Error(), errPrivate.Error()):
		out.Result, out.Detail = "rejected", errPrivate.Error()
	case err != nil:
		out.Result, out.Detail = "unknown", "No delivery answer; reconcile or record a human decision: "+err.Error()
	default:
		answer, readErr := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
		resp.Body.Close()
		if readErr != nil || len(answer) > 64<<10 {
			out.Result, out.Detail = "unknown", "The delivery answer is incomplete or exceeds its retained evidence budget"
			return out
		}
		if len(answer) > 0 && !json.Valid(answer) {
			answer = platform.Raw(clipDetail(answer))
		}
		out.Answer = answer
		code := resp.StatusCode
		switch {
		case code >= 200 && code < 300:
			out.Result = "delivered"
			if req.Protocol != nil {
				var execution platform.DriverExecution
				if json.Unmarshal(answer, &execution) != nil {
					out.Result, out.Detail = "unknown", "The protocol gateway returned no communication result"
					return out
				}
				switch execution.Sending {
				case "answered":
					out.Result = "delivered"
				case "not-sent", "refused":
					out.Result = "rejected"
				default:
					out.Result = "unknown"
				}
				out.Detail = execution.Reason
				if execution.State == "unknown" {
					out.Result = "unknown"
				}
			}
		case code >= 400 && code < 500 && code != 408 && code != 429:
			out.Result, out.Detail = "rejected", resp.Status+": "+clipDetail(answer)
		default:
			out.Result, out.Detail = "retry", resp.Status
		}
	}
	return out
}

func clipDetail(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
