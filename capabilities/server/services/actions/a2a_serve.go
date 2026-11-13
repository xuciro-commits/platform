package actions

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"platformserver/platform"
	"strings"
	"time"
)

const a2aWait = 60 * time.Second

type A2AHooks struct {
	Published func(agent string) bool
	Run       func(id string) (platform.AgentRunRecord, string, bool)
	Decide    func(app, schema, typ, id, key string, payload any) error
}

func A2ACard(d platform.AgentCardDeclaration, agent, tenant, host string, https bool, issuer string) map[string]any {
	scheme := "http"
	if https {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s/a2a/%s/%s", scheme, host, tenant, agent)
	security := map[string]any{"bearer": map[string]any{"httpAuthSecurityScheme": map[string]any{"scheme": "bearer",
		"description": "A token of a member of the tenant; the agent works within their grants. Writes are proposed for confirmation in the workspace."}}}
	name := "bearer"
	if issuer != "" {
		name = "oidc"
		security = map[string]any{"oidc": map[string]any{"openIdConnectSecurityScheme": map[string]any{
			"openIdConnectUrl": strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration",
			"description":      "An access token of a member of the tenant (a service account for machines); the agent acts within their grants."}}}
	}
	description := cmp.Or(d.Description, d.Title+", an agent of the "+d.App+" app.")
	return map[string]any{
		"name": d.Title, "description": description, "version": "1",
		"supportedInterfaces":  []map[string]any{{"url": url, "protocolBinding": "JSONRPC", "protocolVersion": A2AVersion}},
		"provider":             map[string]any{"organization": tenant, "url": fmt.Sprintf("%s://%s", scheme, host)},
		"capabilities":         map[string]any{"streaming": false, "pushNotifications": false},
		"securitySchemes":      security,
		"securityRequirements": []map[string]any{{"schemes": map[string]any{name: map[string]any{"list": []string{}}}}},
		"defaultInputModes":    []string{"text/plain", "application/json"},
		"defaultOutputModes":   []string{"text/plain", "application/json"},
		"skills": []map[string]any{{"id": agent, "name": d.Title, "description": description, "tags": []string{d.App},
			"inputModes": []string{"text/plain"}, "outputModes": []string{"text/plain", "application/json"}}},
	}
}

func A2AServe(body io.Reader, version, agent, member string, h A2AHooks) map[string]any {
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	reply := func(result any, code int, message string) map[string]any {
		out := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if code != 0 {
			out["error"] = map[string]any{"code": code, "message": message}
		} else {
			out["result"] = result
		}
		return out
	}
	if json.NewDecoder(body).Decode(&req) != nil || req.JSONRPC != "2.0" {
		return reply(nil, -32600, "invalid JSON-RPC request")
	}
	if version != "" && !strings.HasPrefix(version, "1.") {
		return reply(nil, -32009, "A2A version "+version+" is not supported; this agent speaks "+A2AVersion)
	}
	if !h.Published(agent) {
		return reply(nil, -32001, "no published agent "+agent)
	}
	decide := func(schema, typ, id, key string, payload any) error {
		app := platform.AgentApp
		if typ == platform.WorkTaskType {
			app = platform.WorkApp
		}
		return h.Decide(app, schema, typ, id, key, payload)
	}
	switch req.Method {
	case "SendMessage":
		var p struct {
			Message       A2AMessage `json:"message"`
			Configuration struct {
				ReturnImmediately bool `json:"returnImmediately"`
			} `json:"configuration"`
		}
		if json.Unmarshal(req.Params, &p) != nil || p.Message.MessageID == "" || strings.TrimSpace(p.Message.Text()) == "" {
			return reply(nil, -32602, "a message with an ID and text is required")
		}
		id := p.Message.TaskID
		if id != "" {
			x, _, ok := h.Run(id)
			if !ok {
				return reply(nil, -32001, "task not found")
			}
			if x.State != "waiting" || !strings.HasPrefix(x.Task, platform.WorkApp+":") && !strings.HasPrefix(x.Task, platform.AgentApp+":") {
				return reply(nil, -32004, "the task is not waiting for input")
			}
			if err := decide("work.task.complete", platform.WorkTaskType, x.Task, "a2a:"+p.Message.MessageID, map[string]string{"answer": p.Message.Text()}); err != nil {
				return reply(nil, -32603, err.Error())
			}
		} else {
			sum := sha256.Sum256([]byte(member + "\x00" + p.Message.MessageID))
			id = "A2A-" + strings.ToUpper(hex.EncodeToString(sum[:6]))
			if err := decide(platform.SchemaRunStart, platform.RunType, id, "a2a:"+p.Message.MessageID, map[string]any{"agent": agent, "goal": p.Message.Text()}); err != nil &&
				!strings.Contains(err.Error(), "CONFLICT") {
				return reply(nil, -32603, err.Error())
			}
		}
		for end := time.Now().Add(a2aWait); !p.Configuration.ReturnImmediately && time.Now().Before(end); time.Sleep(250 * time.Millisecond) {
			if x, _, _ := h.Run(id); x.State != "running" {
				break
			}
		}
		x, question, _ := h.Run(id)
		return reply(map[string]any{"task": TaskOf(x, question)}, 0, "")
	case "GetTask":
		var p struct{ ID string }
		json.Unmarshal(req.Params, &p)
		x, question, ok := h.Run(p.ID)
		if !ok {
			return reply(nil, -32001, "task not found")
		}
		return reply(TaskOf(x, question), 0, "")
	case "CancelTask":
		var p struct{ ID string }
		json.Unmarshal(req.Params, &p)
		x, _, ok := h.Run(p.ID)
		if !ok {
			return reply(nil, -32001, "task not found")
		}
		if x.State == "done" || x.State == "stopped" {
			return reply(nil, -32002, "the task has ended")
		}
		if err := decide(platform.SchemaRunCancel, platform.RunType, x.ID, "a2a-cancel:"+x.ID, map[string]any{}); err != nil {
			return reply(nil, -32603, err.Error())
		}
		x, question, _ := h.Run(p.ID)
		return reply(TaskOf(x, question), 0, "")
	}
	return reply(nil, -32601, "method not found: "+req.Method)
}
