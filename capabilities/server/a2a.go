package platformserver

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/work"
	"platformserver/platform"
)

// Agent-to-agent (ADR-0022 D6, D7), A2A 1.0 over JSON-RPC. A declared agent an
// administrator publishes (the agent app's setting "published") has an agent
// card at /a2a/<tenant>/<agent>/.well-known/agent-card.json and answers at
// /a2a/<tenant>/<agent>. Callers sign in as members through the host's
// issuer; SendMessage starts a run for the caller that acts within their
// grants (no one is there to confirm drafts), and D6 holds what cannot be
// recalled. A question the agent asks is TASK_STATE_INPUT_REQUIRED; the
// caller answers with a message on the task.
const (
	A2AVersion       = "1.0"
	SettingPublished = "published"
	a2aWait          = 60 * time.Second // SendMessage waits this long for a run to finish, unless returnImmediately
)

type a2aPart struct {
	Text      string          `json:"text,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	MediaType string          `json:"mediaType,omitempty"`
}

type a2aMessage struct {
	MessageID string    `json:"messageId"`
	ContextID string    `json:"contextId,omitempty"`
	TaskID    string    `json:"taskId,omitempty"`
	Role      string    `json:"role"`
	Parts     []a2aPart `json:"parts"`
}

type a2aTask struct {
	ID        string `json:"id"`
	ContextID string `json:"contextId"`
	Status    struct {
		State     string      `json:"state"`
		Message   *a2aMessage `json:"message,omitempty"`
		Timestamp time.Time   `json:"timestamp,omitzero"`
	} `json:"status"`
	Artifacts []struct {
		ArtifactID string    `json:"artifactId"`
		Name       string    `json:"name"`
		Parts      []a2aPart `json:"parts"`
	} `json:"artifacts,omitempty"`
}

// text is a message's text parts, and its data parts as JSON.
func (m a2aMessage) text() string {
	var out []string
	for _, p := range m.Parts {
		if p.Text != "" {
			out = append(out, p.Text)
		} else if len(p.Data) > 0 {
			out = append(out, string(p.Data))
		}
	}
	return strings.Join(out, "\n")
}

// published says whether an administrator published the agent over A2A.
func (t *Tenant) published(agent string) bool {
	if t.agents == nil || t.agents.defs[agent] == nil {
		return false
	}
	list := strings.Split(t.setting(t.automation(AgentApp, false), SettingPublished), ",")
	return slices.ContainsFunc(list, func(s string) bool { return strings.TrimSpace(s) == agent })
}

// agentCard is the agent's A2A card.
func (h *Host) agentCard(r *http.Request, t *Tenant, agent string) map[string]any {
	d := t.agents.defs[agent]
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s/a2a/%s/%s", scheme, r.Host, t.ID, agent)
	security := map[string]any{"bearer": map[string]any{"httpAuthSecurityScheme": map[string]any{"scheme": "bearer",
		"description": "A token of a member of the tenant; the agent acts within their grants."}}}
	name := "bearer"
	if h.Issuer != "" {
		name = "oidc"
		security = map[string]any{"oidc": map[string]any{"openIdConnectSecurityScheme": map[string]any{
			"openIdConnectUrl": strings.TrimSuffix(h.Issuer, "/") + "/.well-known/openid-configuration",
			"description":      "An access token of a member of the tenant (a service account for machines); the agent acts within their grants."}}}
	}
	description := cmp.Or(d.Description, d.Title+", an agent of the "+d.app+" app.")
	return map[string]any{
		"name": d.Title, "description": description, "version": "1",
		"supportedInterfaces":  []map[string]any{{"url": url, "protocolBinding": "JSONRPC", "protocolVersion": A2AVersion}},
		"provider":             map[string]any{"organization": t.ID, "url": fmt.Sprintf("%s://%s", scheme, r.Host)},
		"capabilities":         map[string]any{"streaming": false, "pushNotifications": false},
		"securitySchemes":      security,
		"securityRequirements": []map[string]any{{"schemes": map[string]any{name: map[string]any{"list": []string{}}}}},
		"defaultInputModes":    []string{"text/plain", "application/json"},
		"defaultOutputModes":   []string{"text/plain", "application/json"},
		"skills": []map[string]any{{"id": agent, "name": d.Title, "description": description, "tags": []string{d.app},
			"inputModes": []string{"text/plain"}, "outputModes": []string{"text/plain", "application/json"}}},
	}
}

// taskOf is a run as an A2A task.
func taskOf(run AgentRunRecord, question string) a2aTask {
	var x a2aTask
	x.ID, x.ContextID, x.Status.Timestamp = run.ID, run.ID, run.Changed.At
	say := func(text string) *a2aMessage {
		return &a2aMessage{MessageID: run.ID + ":" + fmt.Sprint(len(run.Steps)), TaskID: run.ID, ContextID: run.ID, Role: "ROLE_AGENT", Parts: []a2aPart{{Text: text}}}
	}
	switch run.State {
	case "running":
		x.Status.State = "TASK_STATE_WORKING"
		if len(run.Steps) == 0 {
			x.Status.State = "TASK_STATE_SUBMITTED"
		}
	case "waiting":
		x.Status.State, x.Status.Message = "TASK_STATE_INPUT_REQUIRED", say(question)
	case "done":
		x.Status.State = "TASK_STATE_COMPLETED"
		part := a2aPart{Text: run.Result}
		if json.Valid([]byte(run.Result)) && strings.HasPrefix(strings.TrimSpace(run.Result), "{") {
			part = a2aPart{Data: json.RawMessage(run.Result), MediaType: "application/json"}
		}
		x.Artifacts = append(x.Artifacts, struct {
			ArtifactID string    `json:"artifactId"`
			Name       string    `json:"name"`
			Parts      []a2aPart `json:"parts"`
		}{ArtifactID: run.ID + ":result", Name: "result", Parts: []a2aPart{part}})
	default:
		x.Status.State, x.Status.Message = "TASK_STATE_FAILED", say(run.Stopped)
		if strings.HasPrefix(run.Stopped, "stopped by ") {
			x.Status.State = "TASK_STATE_CANCELED"
		}
	}
	return x
}

// serveA2A answers the JSON-RPC binding for one published agent.
func (h *Host) serveA2A(w http.ResponseWriter, r *http.Request) {
	tenant, agent := r.PathValue("tenant"), r.PathValue("agent")
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	reply := func(result any, code int, message string) {
		out := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if code != 0 {
			out["error"] = map[string]any{"code": code, "message": message}
		} else {
			out["result"] = result
		}
		WriteJSON(w, http.StatusOK, out)
	}
	r.Header.Set(TenantHeader, tenant)
	m, t, ok := h.member(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.JSONRPC != "2.0" {
		reply(nil, -32600, "invalid JSON-RPC request")
		return
	}
	if v := r.Header.Get("A2A-Version"); v != "" && !strings.HasPrefix(v, "1.") {
		reply(nil, -32009, "A2A version "+v+" is not supported; this agent speaks "+A2AVersion)
		return
	}
	if !t.published(agent) {
		reply(nil, -32001, "no published agent "+agent)
		return
	}
	run := func(id string) (AgentRunRecord, string, bool) {
		t.mu.Lock()
		defer t.mu.Unlock()
		x, ok := platform.Get[AgentRunRecord](t.automation(AgentApp, false), id)
		if !ok || x.OnBehalf != m.ID || x.Agent != agent {
			return x, "", false
		}
		question := ""
		if x.State == "waiting" && len(x.Steps) > 0 {
			question = strings.TrimPrefix(x.Steps[len(x.Steps)-1].Outcome, "asked: ")
		}
		return x, question, true
	}
	decide := func(schema, typ, id, key string, payload any) error {
		raw, _ := json.Marshal(payload)
		app := AgentApp
		if typ == work.TaskType {
			app = work.ID
		}
		_, err := t.Submit(m, &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: app, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, h.Now())
		if err != nil {
			return err
		}
		return nil
	}
	switch req.Method {
	case "SendMessage":
		var p struct {
			Message       a2aMessage `json:"message"`
			Configuration struct {
				ReturnImmediately bool `json:"returnImmediately"`
			} `json:"configuration"`
		}
		if json.Unmarshal(req.Params, &p) != nil || p.Message.MessageID == "" || strings.TrimSpace(p.Message.text()) == "" {
			reply(nil, -32602, "a message with an ID and text is required")
			return
		}
		id := p.Message.TaskID
		if id != "" { // an answer to the task's question
			x, _, ok := run(id)
			if !ok {
				reply(nil, -32001, "task not found")
				return
			}
			if x.State != "waiting" || !strings.HasPrefix(x.Task, work.ID+":") && !strings.HasPrefix(x.Task, AgentApp+":") {
				reply(nil, -32004, "the task is not waiting for input")
				return
			}
			if err := decide("work.task.complete", work.TaskType, x.Task, "a2a:"+p.Message.MessageID, map[string]string{"answer": p.Message.text()}); err != nil {
				reply(nil, -32603, err.Error())
				return
			}
		} else {
			sum := sha256.Sum256([]byte(m.ID + "\x00" + p.Message.MessageID))
			id = "A2A-" + strings.ToUpper(hex.EncodeToString(sum[:6]))
			if err := decide(SchemaRunStart, RunType, id, "a2a:"+p.Message.MessageID, map[string]any{"agent": agent, "goal": p.Message.text(), "act": true}); err != nil &&
				!strings.Contains(err.Error(), "CONFLICT") { // a resent message: the same task
				reply(nil, -32603, err.Error())
				return
			}
		}
		for end := time.Now().Add(a2aWait); !p.Configuration.ReturnImmediately && time.Now().Before(end); time.Sleep(250 * time.Millisecond) {
			if x, _, _ := run(id); x.State != "running" {
				break
			}
		}
		x, question, _ := run(id)
		reply(map[string]any{"task": taskOf(x, question)}, 0, "")
	case "GetTask":
		var p struct{ ID string }
		json.Unmarshal(req.Params, &p)
		x, question, ok := run(p.ID)
		if !ok {
			reply(nil, -32001, "task not found")
			return
		}
		reply(taskOf(x, question), 0, "")
	case "CancelTask":
		var p struct{ ID string }
		json.Unmarshal(req.Params, &p)
		x, _, ok := run(p.ID)
		if !ok {
			reply(nil, -32001, "task not found")
			return
		}
		if x.State == "done" || x.State == "stopped" {
			reply(nil, -32002, "the task has ended")
			return
		}
		if err := decide(SchemaRunCancel, RunType, x.ID, "a2a-cancel:"+x.ID, map[string]any{}); err != nil {
			reply(nil, -32603, err.Error())
			return
		}
		x, question, _ := run(p.ID)
		reply(taskOf(x, question), 0, "")
	default:
		reply(nil, -32601, "method not found: "+req.Method)
	}
}

// sendA2A sends an effect to an external agent: a SendMessage whose message ID
// is the effect's, so a resend is the same message; the task it answers with
// is the effect's answer, journaled with the outcome (ADR-0022 D7).
func (t *Tenant) sendA2A(ep Endpoint, x platform.Effect) platform.Outcome {
	sum := sha256.Sum256([]byte(x.Body))
	out := platform.Outcome{Effect: x.ID, Digest: hex.EncodeToString(sum[:])}
	var body struct {
		Data json.RawMessage `json:"data"`
	}
	json.Unmarshal([]byte(x.Body), &body)
	text := string(body.Data)
	var data struct{ Message string }
	if json.Unmarshal(body.Data, &data) == nil && data.Message != "" {
		text = data.Message
	}
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": x.ID, "method": "SendMessage", "params": map[string]any{
		"message": map[string]any{"messageId": x.ID, "role": "ROLE_USER",
			"parts": []map[string]any{{"text": text}, {"data": body.Data, "mediaType": "application/json"}}}}})
	req, _ := http.NewRequest(http.MethodPost, ep.URL, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("A2A-Version", A2AVersion)
	if ep.Secret != "" {
		secret, ok := t.secret(ep.Secret)
		if !ok {
			out.Result, out.Detail = "retry", "secret "+ep.Secret+" missing"
			return out
		}
		req.Header.Set("Authorization", "Bearer "+string(secret))
	}
	send := t.Outbound
	if send == nil {
		send = guarded
	}
	resp, err := send(req, ep.AllowPrivate)
	if err != nil {
		out.Result, out.Detail = "retry", "no answer (resent as the same message): "+err.Error()
		return out
	}
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	var rpc struct {
		Result struct {
			Task    *a2aTask        `json:"task"`
			Message json.RawMessage `json:"message"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	switch {
	case resp.StatusCode >= 500 || resp.StatusCode == 408 || resp.StatusCode == 429:
		out.Result, out.Detail = "retry", resp.Status
	case resp.StatusCode/100 != 2 || json.Unmarshal(answer, &rpc) != nil:
		out.Result, out.Detail = "rejected", resp.Status+": not an A2A answer"
	case rpc.Error != nil:
		out.Result, out.Detail = "rejected", rpc.Error.Message
	case rpc.Result.Message != nil:
		out.Result, out.Answer = "delivered", rpc.Result.Message
	case rpc.Result.Task == nil:
		out.Result, out.Detail = "rejected", "neither a task nor a message"
	default:
		task := rpc.Result.Task
		task.Status.Timestamp = time.Time{}
		switch task.Status.State {
		case "TASK_STATE_COMPLETED":
			out.Result = "delivered"
			out.Answer, _ = json.Marshal(task)
		case "TASK_STATE_SUBMITTED", "TASK_STATE_WORKING":
			out.Result, out.Detail = "retry", "the agent is still working"
		default:
			out.Result, out.Detail = "rejected", strings.TrimPrefix(task.Status.State, "TASK_STATE_")
			if task.Status.Message != nil {
				out.Detail += ": " + task.Status.Message.text()
			}
		}
	}
	return out
}

// answerOf is what an external agent answered, as an agent's tool reads it:
// the artifacts' text and data, or the message's.
func answerOf(o platform.Outcome) string {
	var task a2aTask
	var parts []string
	if json.Unmarshal(o.Answer, &task) == nil && len(task.Artifacts) > 0 {
		for _, a := range task.Artifacts {
			for _, p := range a.Parts {
				parts = append(parts, cmp.Or(p.Text, string(p.Data)))
			}
		}
		return strings.Join(parts, "\n")
	}
	var m a2aMessage
	if json.Unmarshal(o.Answer, &m) == nil && len(m.Parts) > 0 {
		return m.text()
	}
	return string(o.Answer)
}
