package platformserver

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"platformserver/platform"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// MCP serves a member's catalog as Model Context Protocol tools (ADR-0011): each
// action it may call is a tool, and each read it may use is a "read_<name>" tool.
// External agents (Claude Code, other MCP clients) discover and act with the
// member's own grants through the same submission path as every other caller.
// JSON-RPC over HTTP POST; answers are plain JSON (no streaming needed).
func (h *Host) mcp(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ProtocolVersion string         `json:"protocolVersion"`
			Name            string         `json:"name"`
			Arguments       map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		WriteJSON(w, http.StatusBadRequest, rpcError(nil, -32700, "parse error"))
		return
	}
	if req.ID == nil { // a notification (notifications/initialized)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	reply := func(result any) {
		WriteJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	switch req.Method {
	case "initialize":
		version := req.Params.ProtocolVersion
		if version == "" {
			version = "2025-06-18"
		}
		reply(map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":   map[string]string{"name": "platform-host", "version": "1"},
			"instructions": "Actions of the business apps this member may call, and their reads. Every call is checked and recorded like any other."})
	case "ping":
		reply(map[string]any{})
	case "tools/list":
		reply(map[string]any{"tools": toolsFor(t, m)})
	case "tools/call":
		reply(callTool(t, m, req.Params.Name, req.Params.Arguments, h))
	default:
		WriteJSON(w, http.StatusOK, rpcError(req.ID, -32601, "method not found"))
	}
}

func rpcError(id json.RawMessage, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}

func toolName(schema string) string {
	return strings.NewReplacer(".", "_", "/", "_", "#", "_").Replace(schema)
}

func toolsFor(t *Tenant, m platform.Member) []map[string]any {
	tools := []map[string]any{}
	for _, a := range t.Catalog(m) {
		properties := map[string]any{"target": map[string]any{"type": "string", "description": "ID of the " + a.Target + " to act on (a new ID creates one)"}}
		required := []string{"target"}
		for _, f := range a.Payload {
			schema := map[string]any{"type": "string", "description": f.Description}
			switch f.Type {
			case "integer":
				schema["type"] = "integer"
			case "string[]":
				schema = map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": f.Description}
			}
			properties[f.Name] = schema
			if f.Required {
				required = append(required, f.Name)
			}
		}
		tools = append(tools, map[string]any{"name": toolName(a.Schema), "title": a.Title, "description": a.Description,
			"inputSchema": map[string]any{"type": "object", "properties": properties, "required": required}})
	}
	for _, read := range readsFor(t, m) {
		tools = append(tools, map[string]any{"name": "read_" + toolName(read), "title": "Read " + read,
			"description": "The current " + read + " (JSON).", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}})
	}
	return tools
}

func readsFor(t *Tenant, m platform.Member) []string {
	var out []string
	for _, a := range t.apps {
		for _, read := range a.Manifest().Reads {
			if m.Roles[a.Manifest().ID] != "" || slices.Contains(a.Manifest().Everyone, read) {
				out = append(out, read)
			}
		}
	}
	return out
}

func callTool(t *Tenant, m platform.Member, name string, args map[string]any, h *Host) map[string]any {
	text := func(v any, isError bool) map[string]any {
		raw, _ := json.Marshal(v)
		return map[string]any{"content": []map[string]any{{"type": "text", "text": string(raw)}}, "isError": isError}
	}
	if read, ok := strings.CutPrefix(name, "read_"); ok {
		i := slices.IndexFunc(readsFor(t, m), func(r string) bool { return toolName(r) == read })
		if i < 0 {
			return text(map[string]string{"error": "unknown tool"}, true)
		}
		out, err := t.Read(m, readsFor(t, m)[i])
		if err != nil {
			return text(map[string]string{"error": err.Error()}, true)
		}
		return text(out, false)
	}
	catalog := t.Catalog(m)
	i := slices.IndexFunc(catalog, func(a platform.Action) bool { return toolName(a.Schema) == name })
	if i < 0 {
		return text(map[string]string{"error": "not in your catalog"}, true)
	}
	action := catalog[i]
	target, _ := args["target"].(string)
	key, _ := args["idempotencyKey"].(string)
	if key == "" {
		b := make([]byte, 12)
		rand.Read(b)
		key = "mcp:" + base64.RawURLEncoding.EncodeToString(b)
	}
	payload := map[string]any{}
	for k, v := range args {
		if k != "target" && k != "idempotencyKey" {
			payload[k] = v
		}
	}
	raw, _ := json.Marshal(payload)
	record, err := t.Submit(m, &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: t.authorityOf(action.Target),
		Target: &pb.EntityRef{Type: action.Target, Id: target}, Schema: &pb.SchemaRef{Name: action.Schema, Version: 1},
		IdempotencyKey: key, Payload: raw}, h.Now())
	if err != nil {
		return text(map[string]string{"error": err.Error()}, true)
	}
	out, _ := protojson.Marshal(record)
	return text(json.RawMessage(out), false)
}
