package aigateway

import (
	"cmp"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"platformserver/engine/agents"
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

func (g Tools) derived(m platform.Member) map[string]platform.CompiledAgentTool {
	return agents.DerivedTools(g.Host.ModelToolSurface(m), "")
}

func (g Tools) List(m platform.Member) []map[string]any {
	tools := []map[string]any{}
	derived := g.derived(m)
	for _, name := range slices.Sorted(maps.Keys(derived)) {
		tool := derived[name]
		properties := maps.Clone(tool.Tool.Properties)
		required := slices.DeleteFunc(slices.Clone(tool.Tool.Required), func(field string) bool { return field == "rationale" })
		if tool.Kind == "action" || tool.Kind == "function" {
			properties["idempotencyKey"] = map[string]any{"type": "string", "description": "Reuse the same key when retrying this exact action after an unknown response"}
		}
		if required == nil {
			required = []string{}
		}
		tools = append(tools, map[string]any{"name": name, "description": tool.Tool.Description,
			"inputSchema": map[string]any{"type": "object", "properties": properties, "required": required}})
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
	tool, ok := g.derived(m)[name]
	if !ok {
		return text(map[string]string{"error": "not in your catalog"}, true)
	}
	switch tool.Kind {
	case "objects", "aggregate":
		op, raw := agents.ObjectRequest(tool, args)
		out, err := g.Host.AgentObjects(m, op, raw, g.Now())
		if err != nil {
			return text(map[string]string{"error": err.Error()}, true)
		}
		return map[string]any{"content": []map[string]any{{"type": "text", "text": string(out.Body)}}, "isError": false}
	case "action", "function":
	default:
		return text(map[string]string{"error": "not in your catalog"}, true)
	}
	catalog := g.Host.Catalog(m)
	i := slices.IndexFunc(catalog, func(a platform.Action) bool { return a.Schema == tool.Schema })
	if i < 0 {
		return text(map[string]string{"error": "not in your catalog"}, true)
	}
	action := catalog[i]
	if platform.StaticControlAction(action) || strings.HasPrefix(action.Schema, "agent.run.") || action.Schema == platform.SchemaToolPropose {
		return text(map[string]string{"error": "MCP cannot confirm proposals or control agent runs"}, true)
	}
	key, _ := args["idempotencyKey"].(string)
	if key == "" {
		b := make([]byte, 12)
		rand.Read(b)
		key = "mcp:" + base64.RawURLEncoding.EncodeToString(b)
	}
	id := "mcp-" + base64.RawURLEncoding.EncodeToString([]byte(m.ID+":"+key))
	target, _ := args["target"].(string)
	payload := map[string]any{}
	if tool.Kind == "function" {
		payload = maps.Clone(tool.Preset)
		for _, field := range []string{"source", "question"} {
			if value, ok := args[field]; ok {
				payload[field] = value
			}
		}
		target = id + ":f1"
	} else if given, ok := args["payload"].(map[string]any); ok {
		payload = given
	}
	err := g.Host.ProposeModelTool(m, platform.ModelToolProposal{ID: id, Key: key, Action: action, Target: target, Payload: platform.Raw(payload)}, g.Now())
	if err != nil {
		return text(map[string]string{"error": cmp.Or(err.Message, err.Error())}, true)
	}
	return text(map[string]any{"proposal": id, "state": "awaiting-confirmation", "simulation": "accepted", "confirmation": "#/agent-run?id=" + id}, false)
}
