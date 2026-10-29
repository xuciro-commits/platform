package aigateway

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

type Tools struct {
	Host platform.ModelToolHost
	Now  func() time.Time
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

const MCPProtocolVersion = "2025-06-18"

func supportedMCPVersion(version string) bool {
	return version == MCPProtocolVersion || version == "2025-03-26"
}

func (g Tools) Serve(w http.ResponseWriter, r *http.Request, m platform.Member) {
	if version := r.Header.Get("MCP-Protocol-Version"); version != "" && !supportedMCPVersion(version) {
		writeJSON(w, http.StatusBadRequest, rpcError(nil, -32600, "unsupported MCP protocol version"))
		return
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ProtocolVersion string         `json:"protocolVersion"`
			Name            string         `json:"name"`
			Arguments       map[string]any `json:"arguments"`
		} `json:"params"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if decoder.Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, rpcError(nil, -32700, "parse error"))
		return
	}
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	reply := func(result any) {
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	switch req.Method {
	case "initialize":
		version := req.Params.ProtocolVersion
		if !supportedMCPVersion(version) {
			version = MCPProtocolVersion
		}
		reply(map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":   map[string]string{"name": "platform-host", "version": "1"},
			"instructions": "Reads use the member grants. Write tools create simulated proposals; a person confirms them in the workspace. MCP cannot confirm proposals or control runs."})
	case "ping":
		reply(map[string]any{})
	case "tools/list":
		reply(map[string]any{"tools": g.List(m)})
	case "tools/call":
		reply(g.Call(m, req.Params.Name, req.Params.Arguments))
	default:
		writeJSON(w, http.StatusOK, rpcError(req.ID, -32601, "method not found"))
	}
}

func rpcError(id json.RawMessage, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}

func toolName(schema string) string {
	return strings.NewReplacer(".", "_", "/", "_", "#", "_").Replace(schema)
}

func (g Tools) List(m platform.Member) []map[string]any {
	tools := []map[string]any{}
	entities := map[string]platform.EntityInfo{}
	for _, info := range g.Host.Entities(m) {
		entities[info.Type] = info
	}
	for _, a := range g.Host.Catalog(m) {
		if platform.StaticControlAction(a) || strings.HasPrefix(a.Schema, "agent.run.") || a.Schema == platform.SchemaToolPropose {
			continue
		}
		info := entities[a.Target]
		schema := platform.ActionInputSchema(a, &info, append([]string{}, m.RolesIn(info.App)...))
		properties := schema["properties"].(map[string]any)
		properties["target"] = map[string]any{"type": "string", "description": "ID of the " + a.Target + " to act on (a new ID creates one)"}
		properties["idempotencyKey"] = map[string]any{"type": "string", "description": "Reuse the same key when retrying this exact action after an unknown response"}
		schema["required"] = append(schema["required"].([]string), "target")
		tools = append(tools, map[string]any{"name": toolName(a.Schema), "title": a.Title, "description": a.Description,
			"inputSchema": schema})
	}
	for _, read := range g.Host.ModelToolReads(m) {
		tools = append(tools, map[string]any{"name": "read_" + toolName(read), "title": "Read " + read,
			"description": "The current " + read + " (JSON).", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}})
	}
	return tools
}

func (g Tools) Call(m platform.Member, name string, args map[string]any) map[string]any {
	text := func(v any, isError bool) map[string]any {
		raw, _ := json.Marshal(v)
		return map[string]any{"content": []map[string]any{{"type": "text", "text": string(raw)}}, "isError": isError}
	}
	if read, ok := strings.CutPrefix(name, "read_"); ok {
		reads := g.Host.ModelToolReads(m)
		i := slices.IndexFunc(reads, func(r string) bool { return toolName(r) == read })
		if i < 0 {
			return text(map[string]string{"error": "unknown tool"}, true)
		}
		out, err := g.Host.Read(m, reads[i])
		if err != nil {
			return text(map[string]string{"error": err.Error()}, true)
		}
		return text(out, false)
	}
	catalog := g.Host.Catalog(m)
	i := slices.IndexFunc(catalog, func(a platform.Action) bool { return toolName(a.Schema) == name })
	if i < 0 {
		return text(map[string]string{"error": "not in your catalog"}, true)
	}
	action := catalog[i]
	if platform.StaticControlAction(action) || strings.HasPrefix(action.Schema, "agent.run.") || action.Schema == platform.SchemaToolPropose {
		return text(map[string]string{"error": "MCP cannot confirm proposals or control agent runs"}, true)
	}
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
	id := "mcp-" + base64.RawURLEncoding.EncodeToString([]byte(m.ID+":"+key))
	err := g.Host.ProposeModelTool(m, platform.ModelToolProposal{ID: id, Key: key, Action: action, Target: target, Payload: platform.Raw(payload)}, g.Now())
	if err != nil {
		return text(map[string]string{"error": err.Error()}, true)
	}
	return text(map[string]any{"proposal": id, "state": "awaiting-confirmation", "simulation": "accepted", "confirmation": "#/agent-run?id=" + id}, false)
}
