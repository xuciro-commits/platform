package platformserver

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Outbound effects (ADR-0014). An intent is created inside an input, so replay
// rebuilds it; an attempt is made by the dispatcher outside the tenant's lock
// and never during replay; its outcome is journaled as an input of its own.
// This is the K5 outbox with the host as the edge and the endpoint as the
// authority: at least once, with a key the receiver deduplicates by.
//
// The first use is webhooks (D7): an administrator subscribes an endpoint to
// events in Settings, and the platform app turns each matching event into an
// effect, signed as Standard Webhooks.

// Endpoint is a destination the tenant's administrator configured. A webhook
// receives the events it subscribes to (no app code) and the effects apps emit
// of the kinds bound to it ("<app>/<kind>", e.g. "erpadapter/confirmation"). An
// email endpoint is an SMTP server that mails members the notifications of the
// apps it names (mail.go).
type Endpoint struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`             // webhook, email, a2a
	URL           string   `json:"url"`              // https://… or smtp://[user@]host[:port]
	Secret        string   `json:"secret,omitempty"` // a secret's name in the store, never the secret
	Events        []string `json:"events,omitempty"` // action schemas or "<protocol id>#<event>"
	Effects       []string `json:"effects,omitempty"`
	From          string   `json:"from,omitempty"`          // email: the sender
	Notifications []string `json:"notifications,omitempty"` // email: apps whose notifications it mails
	AllowPrivate  bool     `json:"allowPrivate,omitempty"`
	// Irreversible: what is sent here cannot be recalled (an external agent
	// that acts), so an agent's effects wait for a person whatever their kind.
	Irreversible bool `json:"irreversible,omitempty"`
}

// effect is an Effect with the dispatcher's bookkeeping.
type effect struct {
	platform.Effect
	since   int // attempts before the last manual retry: each retry gets a full schedule
	sending bool
}

// effectRetry is how an effect tries again: 5 s … 1 h apart, about seven hours, then failed.
var effectRetry = platform.Retry{Initial: 5 * time.Second, Max: time.Hour, Attempts: 12}

const (
	effectsKept   = 1000
	bodyKept      = 30 * 24 * time.Hour
	effectTimeout = 10 * time.Second
)

// effectBackoff grows from 5 s to an hour, with jitter derived from the key so
// a replay computes the same due time.
func effectBackoff(id string, attempts int) time.Duration {
	d := effectRetry.After(attempts)
	h := fnv.New32a()
	h.Write([]byte(id + strconv.Itoa(attempts)))
	return d + time.Duration(h.Sum32()%1000)*d/5000 // up to +20 %
}

func settled(state string) bool {
	return state == "delivered" || state == "rejected" || state == "failed" || state == "discarded"
}

// emit turns an accepted decision into effects for every endpoint subscribed to
// one of its names. It runs inside the input and inside replay.
func (t *Tenant) emit(e platform.Event, names []string) {
	s := e.Record.GetSubmission()
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, ep := range t.endpoints {
		i := slices.IndexFunc(names, func(n string) bool { return slices.Contains(ep.Events, n) })
		if i < 0 {
			continue
		}
		var payload any = string(s.GetPayload())
		if json.Valid(s.GetPayload()) {
			payload = json.RawMessage(s.GetPayload())
		}
		at := e.Record.GetRecordedTime().AsTime()
		body, _ := json.Marshal(map[string]any{"type": names[i], "timestamp": at, "data": map[string]any{
			"app": e.App, "action": s.GetSchema().GetName(), "schemaVersion": s.GetSchema().GetVersion(), "entity": target(s), "changeId": e.Record.GetChangeId(),
			"principal": s.GetPrincipalId(), "revision": e.Record.GetRevision(), "payload": payload}})
		t.outbound = append(t.outbound, &effect{Effect: platform.Effect{ID: fmt.Sprintf("%s:%s:%s:%s", t.ID, e.App, e.Record.GetChangeId(), ep.ID),
			Endpoint: ep.ID, Event: names[i], Target: target(s), At: at, State: "pending", Due: at, Body: string(body)}})
	}
	t.trimEffects()
}

func (t *Tenant) trimEffects() {
	for len(t.outbound) > effectsKept {
		i := slices.IndexFunc(t.outbound, func(x *effect) bool { return settled(x.State) })
		if i < 0 {
			return
		}
		t.outbound = slices.Delete(t.outbound, i, i+1)
	}
}

// emitFor makes c's app's effect of kind for every endpoint bound to it (Caller.Emit).
func (t *Tenant) emitFor(c platform.Caller, kind, key, entity string, data any, now time.Time) (int, *kernel.Error) {
	a := t.app(c.App)
	if a == nil || key == "" {
		return 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	i := slices.IndexFunc(a.Manifest().Emits, func(e platform.EffectKind) bool { return e.Name == kind })
	if i < 0 {
		return 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	agent, run := "", ""
	if c.Agent {
		agent, run = c.ID, t.agentRun
	}
	name := c.App + "/" + kind
	body, _ := json.Marshal(map[string]any{"type": name, "timestamp": now, "data": data})
	t.opsMu.Lock()
	n, held := 0, []string{}
	for _, ep := range t.endpoints {
		id := fmt.Sprintf("%s:%s:%s:%s:%s", t.ID, c.App, kind, key, ep.ID)
		if !slices.Contains(ep.Effects, name) || slices.ContainsFunc(t.outbound, func(x *effect) bool { return x.ID == id }) {
			continue
		}
		state := "pending"
		if c.Agent && (a.Manifest().Emits[i].Irreversible || ep.Irreversible) {
			state = "held"
		}
		t.outbound = append(t.outbound, &effect{Effect: platform.Effect{ID: id, Endpoint: ep.ID, Event: name, App: c.App, Key: key, Target: entity, At: now,
			State: state, Agent: agent, Run: run, Due: now, Body: string(body)}})
		n++
		if state == "held" {
			held = append(held, id)
		}
	}
	t.trimEffects()
	t.opsMu.Unlock()
	// The tenant's administrators are asked, as the platform app, inside the input: replay asks again.
	console := platform.NewCaller(runtime{t}, platform.Member{ID: "app:" + PlatformApp, Tenant: t.ID, Roles: map[string]string{}}, PlatformApp, c.Replaying, true)
	for _, id := range held {
		t.notify(console, platform.Notification{Title: fmt.Sprintf("Approve %s for %s", a.Manifest().Emits[i].Title, entity),
			Body: fmt.Sprintf("The AI agent %s caused it, and it cannot be recalled once sent. Approve or discard it in Settings → Integrations.", c.ID),
			Ref:  EffectType + "/" + id, Key: "approve:" + id}, now, []platform.Recipient{{AppRole: Admin}})
	}
	return n, nil
}

// Effects lists the tenant's effects, newest first; bodies older than 30 days are dropped.
func (t *Tenant) Effects(now time.Time) []platform.Effect {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	out := []platform.Effect{}
	for i := len(t.outbound) - 1; i >= 0; i-- {
		x := t.outbound[i].Effect
		if now.Sub(x.At) > bodyKept {
			x.Body = ""
		}
		out = append(out, x)
	}
	return out
}

// EndpointView is an endpoint with how its effects stand.
type EndpointView struct {
	Endpoint
	Pending  int    `json:"pending"`
	Failing  int    `json:"failing"` // consecutive failed attempts of its head
	Health   string `json:"health"`  // ok, failing, secret missing
	Delivers int    `json:"delivered"`
}

func (t *Tenant) Endpoints() []EndpointView {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	out := []EndpointView{}
	for _, ep := range t.endpoints {
		v := EndpointView{Endpoint: *ep, Health: "ok"}
		for _, x := range t.outbound {
			switch {
			case x.Endpoint != ep.ID:
			case x.State == "delivered":
				v.Delivers++
			case !settled(x.State):
				v.Pending++
				if x.State == "retrying" && v.Failing == 0 {
					v.Failing, v.Health = x.Attempts, "failing"
				}
			}
		}
		if _, ok := t.secret(ep.Secret); !ok && ep.Secret != "" {
			v.Health = "secret missing"
		}
		out = append(out, v)
	}
	return out
}

// secret looks a secret up by name (D5): a file in $PLATFORM_SECRETS_DIR, or
// the variable PLATFORM_SECRET_<NAME>. Tests set Tenant.Secrets instead.
func (t *Tenant) secret(name string) ([]byte, bool) {
	if t.Secrets != nil {
		return t.Secrets(name)
	}
	if dir := os.Getenv("PLATFORM_SECRETS_DIR"); dir != "" {
		if raw, err := os.ReadFile(filepath.Join(dir, filepath.Base(name))); err == nil {
			return bytes.TrimSpace(raw), true
		}
	}
	v, ok := os.LookupEnv("PLATFORM_SECRET_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(name)))
	return []byte(v), ok && v != ""
}

// Dispatch makes the attempts that are due at now: the head of each endpoint's
// effects (ordered per endpoint, D2). It sends outside every lock and journals
// each outcome. The host calls it every second; it is never called in replay.
func (t *Tenant) Dispatch(now time.Time) { onLane(t.dispatches(now)) }

// dispatches are the attempts due at now, to run on the I/O lane: endpoints
// side by side, each in its order; an endpoint whose breaker is open waits,
// spending no attempts.
func (t *Tenant) dispatches(now time.Time) []func() {
	type job struct {
		effect   platform.Effect
		endpoint Endpoint
	}
	var jobs []job
	t.opsMu.Lock()
	for _, ep := range t.endpoints {
		if !t.breakers.allow("endpoint:"+ep.ID, now) {
			continue
		}
		// A held effect waits for its approval outside the endpoint's order.
		i := slices.IndexFunc(t.outbound, func(x *effect) bool { return x.Endpoint == ep.ID && !settled(x.State) && x.State != "held" })
		if i < 0 || t.outbound[i].sending || t.outbound[i].Due.After(now) {
			continue
		}
		t.outbound[i].sending = true
		jobs = append(jobs, job{t.outbound[i].Effect, *ep})
	}
	t.opsMu.Unlock()
	var sends []func()
	for _, j := range jobs {
		sends = append(sends, func() {
			outcome := t.send(j.endpoint, j.effect, now)
			t.breakers.report("endpoint:"+j.endpoint.ID, outcome.Result != "retry", now)
			t.settle(j.effect.ID, outcome, now)
		})
	}
	return sends
}

// send makes one attempt, signed as Standard Webhooks, with the effect's ID as
// both webhook-id and Idempotency-Key.
func (t *Tenant) send(ep Endpoint, x platform.Effect, now time.Time) platform.Outcome {
	if ep.Kind == "email" {
		return t.sendMail(ep, x, now)
	}
	if ep.Kind == "a2a" {
		return t.sendA2A(ep, x)
	}
	sum := sha256.Sum256([]byte(x.Body))
	out := platform.Outcome{Effect: x.ID, Digest: hex.EncodeToString(sum[:])}
	secret, ok := t.secret(ep.Secret)
	if !ok {
		out.Result, out.Detail = "retry", "secret "+ep.Secret+" missing"
		return out
	}
	u, err := url.Parse(ep.URL)
	if err != nil || u.Scheme != "https" && !(u.Scheme == "http" && ep.AllowPrivate) {
		out.Result, out.Detail = "rejected", "only https endpoints (http when private addresses are allowed)"
		return out
	}
	stamp := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(x.ID + "." + stamp + "." + x.Body))
	req, _ := http.NewRequest(http.MethodPost, ep.URL, strings.NewReader(x.Body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("webhook-id", x.ID)
	req.Header.Set("webhook-timestamp", stamp)
	req.Header.Set("webhook-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Idempotency-Key", x.ID)
	send := t.Outbound
	if send == nil {
		send = guarded
	}
	resp, err := send(req, ep.AllowPrivate)
	switch {
	case err != nil && strings.Contains(err.Error(), errPrivate.Error()):
		out.Result, out.Detail = "rejected", errPrivate.Error()
	case err != nil:
		out.Result, out.Detail = "retry", "no answer (unknown; resent with the same key): "+err.Error()
	default:
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if x.App != "" && json.Valid(answer) {
			out.Answer = answer
		}
		code := resp.StatusCode
		switch {
		case code >= 200 && code < 300:
			out.Result = "delivered"
		case code >= 400 && code < 500 && code != 408 && code != 429:
			out.Result, out.Detail = "rejected", resp.Status
		default:
			out.Result, out.Detail = "retry", resp.Status
		}
	}
	return out
}

var errPrivate = fmt.Errorf("private address refused")

// guarded sends with a dialer that refuses private, loopback and link-local
// addresses at connect time (after DNS, so a rebinding name cannot slip through).
func guarded(req *http.Request, allowPrivate bool) (*http.Response, error) {
	dialer := guardedDialer(allowPrivate)
	client := &http.Client{Timeout: effectTimeout, Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(req.Context(), effectTimeout)
	defer cancel()
	return client.Do(req.WithContext(ctx))
}

func guardedDialer(allowPrivate bool) *net.Dialer {
	return &net.Dialer{Timeout: effectTimeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, _ := net.SplitHostPort(address)
		ip := net.ParseIP(host)
		if !allowPrivate && (ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
			return errPrivate
		}
		return nil
	}}
}

// settle journals an attempt's outcome, then applies it.
func (t *Tenant) settle(effect string, o platform.Outcome, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	body, _ := json.Marshal(o)
	if p := t.app(PlatformApp); p != nil {
		t.record(p, "effect", t.automation(PlatformApp, false).Member, body, now)
	}
	t.apply(o, now, false)
}

// apply records an outcome on its effect and, once the effect is settled, tells
// the app that emitted it; replay calls it with the journaled outcome.
func (t *Tenant) apply(o platform.Outcome, at time.Time, replaying bool) bool {
	x, ok := t.mark(o, at)
	if !ok {
		return false
	}
	if x.State == "failed" {
		t.failedWork("Effect to "+x.Endpoint+" failed: "+x.Event, x.Error, x.ID, at, replaying)
	}
	if x.App != "" && settled(x.State) && x.State != "discarded" {
		if a, ok := t.app(x.App).(platform.Answerer); ok {
			a.Answer(t.automation(x.App, replaying), x, o, at)
			t.enqueue(at)
		}
		if t.agents != nil { // an agent that sent it waits for the answer (ADR-0022 D7)
			t.agents.effectEnded(t.automation(AgentApp, replaying), nil, x, o, at)
			t.enqueue(at)
		}
	}
	return true
}

func (t *Tenant) mark(o platform.Outcome, at time.Time) (platform.Effect, bool) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	i := slices.IndexFunc(t.outbound, func(x *effect) bool { return x.ID == o.Effect })
	if i < 0 {
		return platform.Effect{}, false
	}
	x := t.outbound[i]
	x.sending, x.Last, x.Digest, x.Error = false, at, o.Digest, o.Detail
	if x.State == "discarded" {
		return x.Effect, true // discarded while its attempt was on the way
	}
	x.Attempts++
	switch {
	case o.Result == "delivered":
		x.State = "delivered"
	case o.Result == "rejected":
		x.State = "rejected"
	case x.Attempts-x.since >= effectRetry.Attempts:
		x.State = "failed"
	default:
		x.State, x.Due = "retrying", at.Add(effectBackoff(x.ID, x.Attempts-x.since))
	}
	return x.Effect, true
}

// The console's actions about endpoints and effects.
const (
	EndpointType         = "platform.endpoint"
	EffectType           = "platform.effect"
	SchemaEndpointAdd    = "platform.endpoint.add"
	SchemaEndpointRemove = "platform.endpoint.remove"
	SchemaEffectRetry    = "platform.effect.retry"
	SchemaEffectDiscard  = "platform.effect.discard"
	SchemaEffectApprove  = "platform.effect.approve"
)

func effectActions() []platform.Action {
	admin := []string{Admin}
	return []platform.Action{
		{Schema: SchemaEndpointAdd, Target: EndpointType, Capability: "integrations", Title: "Add endpoint",
			Description: "Send the chosen events and effects to a URL, signed with a named secret (Standard Webhooks); or mail the chosen apps' notifications to members through an SMTP server.",
			Payload: []platform.Field{{Name: "url", Type: "string", Required: true, Description: "https URL of the receiver"},
				{Name: "secret", Type: "string", Required: true, Description: "Name of the signing secret in the secret store"},
				{Name: "events", Type: "string[]", Description: "Action schemas or <protocol id>#<event> to send as webhooks"},
				{Name: "effects", Type: "string[]", Description: "Effect kinds apps emit, <app>/<kind>, to send here"},
				{Name: "kind", Type: "string", Description: "webhook (default), email, or a2a (an external agent's A2A 1.0 JSON-RPC URL; the secret is its bearer token)"},
				{Name: "irreversible", Type: "boolean", Description: "What the receiver does cannot be recalled: effects agents cause wait for a person"},
				{Name: "from", Type: "string", Description: "email: the sender address"},
				{Name: "notifications", Type: "string[]", Description: "email: apps whose notifications are mailed to members"},
				{Name: "allowPrivate", Type: "boolean", Description: "Allow private addresses and plain http (inside the deployment only)"}}, Roles: admin},
		{Schema: SchemaEndpointRemove, Target: EndpointType, Capability: "integrations", Title: "Remove endpoint",
			Description: "Stop sending to the endpoint; what it has not received is discarded.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaEffectRetry, Target: EffectType, Capability: "integrations", Title: "Retry effect",
			Description: "Send a failed or rejected effect again, with the same key.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaEffectDiscard, Target: EffectType, Capability: "integrations", Title: "Discard effect",
			Description: "Give up on an effect not yet delivered.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaEffectApprove, Target: EffectType, Capability: "integrations", Title: "Approve effect",
			Description: "Send an irreversible effect an AI agent caused; only a person may approve it (ADR-0014 D6).", Payload: []platform.Field{}, Roles: admin},
	}
}

// decideEndpoint decides adding and removing an endpoint.
func (t *Tenant) decideEndpoint(_ platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	t.opsMu.Lock()
	known := slices.IndexFunc(t.endpoints, func(e *Endpoint) bool { return e.ID == id }) >= 0
	t.opsMu.Unlock()
	if s.GetSchema().GetName() == SchemaEndpointRemove {
		if !known {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return func(*pb.ChangeRecord) {
			t.opsMu.Lock()
			defer t.opsMu.Unlock()
			t.endpoints = slices.DeleteFunc(t.endpoints, func(e *Endpoint) bool { return e.ID == id })
			for _, x := range t.outbound {
				if x.Endpoint == id && !settled(x.State) {
					x.State = "discarded"
				}
			}
		}, nil
	}
	var ep Endpoint
	if json.Unmarshal(s.GetPayload(), &ep) != nil || id == "" {
		return nil, invalid
	}
	u, err := url.Parse(ep.URL)
	if err != nil || u.Host == "" {
		return nil, invalid
	}
	switch ep.Kind {
	case "", "webhook":
		if ep.Secret == "" || len(ep.Events)+len(ep.Effects) == 0 || len(ep.Notifications) > 0 || u.Scheme != "https" && !(u.Scheme == "http" && ep.AllowPrivate) {
			return nil, invalid
		}
		ep.Kind = "webhook"
	case "a2a":
		if len(ep.Effects) == 0 || len(ep.Events)+len(ep.Notifications) > 0 || u.Scheme != "https" && !(u.Scheme == "http" && ep.AllowPrivate) {
			return nil, invalid
		}
	case "email":
		if u.Scheme != "smtp" || !strings.Contains(ep.From, "@") || len(ep.Notifications) == 0 || len(ep.Events)+len(ep.Effects) > 0 || u.User != nil && ep.Secret == "" {
			return nil, invalid
		}
		for _, app := range ep.Notifications {
			if t.app(app) == nil {
				return nil, invalid
			}
		}
	default:
		return nil, invalid
	}
	for _, event := range ep.Events { // an administrator's endpoint may receive any event the tenant declares (G5)
		if _, declared := t.protocolEvent(event); t.owner["action:"+event] == nil && !declared {
			return nil, invalid
		}
	}
	for _, kind := range ep.Effects {
		app, name, _ := strings.Cut(kind, "/")
		if a := t.app(app); a == nil || !slices.ContainsFunc(a.Manifest().Emits, func(e platform.EffectKind) bool { return e.Name == name }) {
			return nil, invalid
		}
	}
	if known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	ep.ID = id
	return func(*pb.ChangeRecord) { t.opsMu.Lock(); t.endpoints = append(t.endpoints, &ep); t.opsMu.Unlock() }, nil
}

// decideEffect decides retrying, discarding and approving an effect.
func (t *Tenant) decideEffect(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	t.opsMu.Lock()
	var x *effect
	if i := slices.IndexFunc(t.outbound, func(x *effect) bool { return x.ID == id }); i >= 0 {
		x = t.outbound[i]
	}
	t.opsMu.Unlock()
	if s.GetSchema().GetName() == SchemaEffectApprove {
		if x == nil || x.State != "held" {
			return nil, notFound
		}
		if c.Agent { // D6: a person approves what an agent caused
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return func(r *pb.ChangeRecord) {
			t.opsMu.Lock()
			x.State, x.Due = "pending", now
			t.opsMu.Unlock()
			if x.Run != "" && t.agents != nil {
				t.agents.signal(c, r, x.Run, Signal{At: now, Kind: "approved", By: c.ID, Detail: x.Event}, now)
			}
		}, nil
	}
	if s.GetSchema().GetName() == SchemaEffectDiscard {
		if x == nil || settled(x.State) {
			return nil, notFound
		}
		return func(r *pb.ChangeRecord) {
			t.opsMu.Lock()
			held := x.State == "held"
			x.State = "discarded"
			t.opsMu.Unlock()
			if held && x.Run != "" && t.agents != nil { // a person refused what the agent caused (ADR-0022 D9)
				t.agents.signal(c, r, x.Run, Signal{At: now, Kind: "discarded", By: c.ID, Detail: x.Event}, now)
			}
			if t.agents != nil {
				t.agents.effectEnded(c, r, x.Effect, platform.Outcome{Result: "discarded", Detail: "discarded by " + c.ID}, now)
			}
		}, nil
	}
	if x == nil || (x.State != "failed" && x.State != "rejected") {
		return nil, notFound
	}
	return func(*pb.ChangeRecord) {
		t.opsMu.Lock()
		x.State, x.Due, x.since = "pending", now, x.Attempts
		t.opsMu.Unlock()
	}, nil
}
