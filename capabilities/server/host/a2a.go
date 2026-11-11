package host

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"platformserver/services/actions"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const a2aWait = 60 * time.Second

func (t *Tenant) published(agent string) bool {
	if t.agents == nil {
		return false
	}
	if _, ok := t.agents.AgentDeclaration(agent); !ok {
		return false
	}
	list := strings.Split(t.setting(t.automation(platform.AgentApp, false), platform.SettingPublished), ",")
	return slices.ContainsFunc(list, func(s string) bool { return strings.TrimSpace(s) == agent })
}

func (h *Host) agentCard(r *http.Request, t *Tenant, agent string) map[string]any {
	d, _ := t.agents.AgentDeclaration(agent)
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s/a2a/%s/%s", scheme, r.Host, t.ID, agent)
	security := map[string]any{"bearer": map[string]any{"httpAuthSecurityScheme": map[string]any{"scheme": "bearer",
		"description": "A token of a member of the tenant; the agent works within their grants. Writes are proposed for confirmation in the workspace."}}}
	name := "bearer"
	if h.Issuer != "" {
		name = "oidc"
		security = map[string]any{"oidc": map[string]any{"openIdConnectSecurityScheme": map[string]any{
			"openIdConnectUrl": strings.TrimSuffix(h.Issuer, "/") + "/.well-known/openid-configuration",
			"description":      "An access token of a member of the tenant (a service account for machines); the agent acts within their grants."}}}
	}
	description := cmp.Or(d.Description, d.Title+", an agent of the "+d.App+" app.")
	return map[string]any{
		"name": d.Title, "description": description, "version": "1",
		"supportedInterfaces":  []map[string]any{{"url": url, "protocolBinding": "JSONRPC", "protocolVersion": actions.A2AVersion}},
		"provider":             map[string]any{"organization": t.ID, "url": fmt.Sprintf("%s://%s", scheme, r.Host)},
		"capabilities":         map[string]any{"streaming": false, "pushNotifications": false},
		"securitySchemes":      security,
		"securityRequirements": []map[string]any{{"schemes": map[string]any{name: map[string]any{"list": []string{}}}}},
		"defaultInputModes":    []string{"text/plain", "application/json"},
		"defaultOutputModes":   []string{"text/plain", "application/json"},
		"skills": []map[string]any{{"id": agent, "name": d.Title, "description": description, "tags": []string{d.App},
			"inputModes": []string{"text/plain"}, "outputModes": []string{"text/plain", "application/json"}}},
	}
}

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
		reply(nil, -32009, "A2A version "+v+" is not supported; this agent speaks "+actions.A2AVersion)
		return
	}
	if !t.published(agent) {
		reply(nil, -32001, "no published agent "+agent)
		return
	}
	run := func(id string) (platform.AgentRunRecord, string, bool) {
		type answer struct {
			run      platform.AgentRunRecord
			question string
			known    bool
		}
		value, refusal := recordRead(t, r.Context(), func(t *Tenant) (answer, *kernel.Error) {
			x, ok := platform.Get[platform.AgentRunRecord](t.automation(platform.AgentApp, false), id)
			if !ok || x.OnBehalf != m.ID || x.Agent != agent {
				return answer{}, nil
			}
			question := ""
			if x.State == "waiting" && len(x.Steps) > 0 {
				question = strings.TrimPrefix(x.Steps[len(x.Steps)-1].Outcome, "asked: ")
			}
			return answer{x, question, true}, nil
		})
		return value.run, value.question, value.known && refusal == nil
	}
	decide := func(schema, typ, id, key string, payload any) error {
		raw, _ := json.Marshal(payload)
		app := platform.AgentApp
		if typ == platform.WorkTaskType {
			app = platform.WorkApp
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
			Message       actions.A2AMessage `json:"message"`
			Configuration struct {
				ReturnImmediately bool `json:"returnImmediately"`
			} `json:"configuration"`
		}
		if json.Unmarshal(req.Params, &p) != nil || p.Message.MessageID == "" || strings.TrimSpace(p.Message.Text()) == "" {
			reply(nil, -32602, "a message with an ID and text is required")
			return
		}
		id := p.Message.TaskID
		if id != "" {
			x, _, ok := run(id)
			if !ok {
				reply(nil, -32001, "task not found")
				return
			}
			if x.State != "waiting" || !strings.HasPrefix(x.Task, platform.WorkApp+":") && !strings.HasPrefix(x.Task, platform.AgentApp+":") {
				reply(nil, -32004, "the task is not waiting for input")
				return
			}
			if err := decide("work.task.complete", platform.WorkTaskType, x.Task, "a2a:"+p.Message.MessageID, map[string]string{"answer": p.Message.Text()}); err != nil {
				reply(nil, -32603, err.Error())
				return
			}
		} else {
			sum := sha256.Sum256([]byte(m.ID + "\x00" + p.Message.MessageID))
			id = "A2A-" + strings.ToUpper(hex.EncodeToString(sum[:6]))
			if err := decide(platform.SchemaRunStart, platform.RunType, id, "a2a:"+p.Message.MessageID, map[string]any{"agent": agent, "goal": p.Message.Text()}); err != nil &&
				!strings.Contains(err.Error(), "CONFLICT") {
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
		reply(map[string]any{"task": actions.TaskOf(x, question)}, 0, "")
	case "GetTask":
		var p struct{ ID string }
		json.Unmarshal(req.Params, &p)
		x, question, ok := run(p.ID)
		if !ok {
			reply(nil, -32001, "task not found")
			return
		}
		reply(actions.TaskOf(x, question), 0, "")
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
		if err := decide(platform.SchemaRunCancel, platform.RunType, x.ID, "a2a-cancel:"+x.ID, map[string]any{}); err != nil {
			reply(nil, -32603, err.Error())
			return
		}
		x, question, _ := run(p.ID)
		reply(actions.TaskOf(x, question), 0, "")
	default:
		reply(nil, -32601, "method not found: "+req.Method)
	}
}
