package gateway

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Tools struct {
	Host runtimeprotocol.ModelToolHost
	Now  func() time.Time
}

type mcpClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type mcpCallMeta struct {
	Model string `json:"model"`
	Event string `json:"event"`
	Run   string `json:"run"`
	Usage struct {
		Input  int     `json:"input"`
		Output int     `json:"output"`
		Cached int     `json:"cached"`
		Cost   float64 `json:"cost"`
	} `json:"usage"`
}

var mcpSessions struct {
	mu      sync.Mutex
	order   []string
	clients map[string]mcpClientInfo
}

func storeMCPSession(tenant, member, session string, client mcpClientInfo) {
	mcpSessions.mu.Lock()
	defer mcpSessions.mu.Unlock()
	if mcpSessions.clients == nil {
		mcpSessions.clients = map[string]mcpClientInfo{}
	}
	key := tenant + "\x00" + member + "\x00" + session
	if _, exists := mcpSessions.clients[key]; !exists {
		if len(mcpSessions.order) >= 512 {
			oldest := mcpSessions.order[0]
			mcpSessions.order = mcpSessions.order[1:]
			delete(mcpSessions.clients, oldest)
		}
		mcpSessions.order = append(mcpSessions.order, key)
	}
	mcpSessions.clients[key] = client
}

func loadMCPSession(tenant, member, session string) (mcpClientInfo, bool) {
	if session == "" {
		return mcpClientInfo{}, false
	}
	mcpSessions.mu.Lock()
	defer mcpSessions.mu.Unlock()
	c, ok := mcpSessions.clients[tenant+"\x00"+member+"\x00"+session]
	return c, ok
}

func deleteMCPSession(tenant, member, session string) {
	if session == "" {
		return
	}
	mcpSessions.mu.Lock()
	defer mcpSessions.mu.Unlock()
	delete(mcpSessions.clients, tenant+"\x00"+member+"\x00"+session)
}

func ServeMCPMethod(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", "POST, DELETE")
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

const (
	MCPProtocolVersion = "2025-06-18"
	mcpToolNameLimit   = 64
	mcpMaxFieldsInDesc = 40
)

func supportedMCPVersion(version string) bool {
	return version == MCPProtocolVersion || version == "2025-03-26" || version == "2024-11-05"
}

func (g Tools) Serve(w http.ResponseWriter, r *http.Request, m platform.Member) {
	if r.Method == http.MethodDelete {
		deleteMCPSession(g.Host.TenantID(), m.ID, strings.TrimSpace(r.Header.Get("Mcp-Session-Id")))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if version := r.Header.Get("MCP-Protocol-Version"); version != "" && !supportedMCPVersion(version) {
		writeJSON(w, http.StatusBadRequest, rpcError(nil, -32600, "unsupported MCP protocol version"))
		return
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ProtocolVersion string         `json:"protocolVersion"`
			ClientInfo      mcpClientInfo  `json:"clientInfo"`
			Name            string         `json:"name"`
			Arguments       map[string]any `json:"arguments"`
			Meta            mcpCallMeta    `json:"_meta"`
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
	session := strings.TrimSpace(r.Header.Get("Mcp-Session-Id"))
	client, _ := loadMCPSession(g.Host.TenantID(), m.ID, session)
	if client.Name == "" && r.UserAgent() != "" {
		client.Name = r.UserAgent()
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
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		session = "mcp-" + base64.RawURLEncoding.EncodeToString(b)
		info := mcpClientInfo{
			Name:    cmp.Or(strings.TrimSpace(req.Params.ClientInfo.Name), strings.TrimSpace(r.UserAgent()), "mcp-client"),
			Version: strings.TrimSpace(req.Params.ClientInfo.Version),
		}
		storeMCPSession(g.Host.TenantID(), m.ID, session, info)
		w.Header().Set("Mcp-Session-Id", session)
		reply(map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]string{"name": "platform-host", "version": "1"},
			"instructions":    "Tools are generated from your readable object types and authorized published actions. Actions marked ask hold a question in your inbox until a person approves.",
		})
	case "ping":
		reply(map[string]any{})
	case "tools/list":
		reply(map[string]any{"tools": g.List(withMCPCaller(m, client, "", mcpCallMeta{}))})
	case "tools/call":
		reply(g.Call(r.Context(), requestOrigin(r), withMCPCaller(m, client, req.Params.Name, req.Params.Meta), req.Params.Name, req.Params.Arguments))
	default:
		writeJSON(w, http.StatusOK, rpcError(req.ID, -32601, "method not found"))
	}
}

func withMCPCaller(m platform.Member, client mcpClientInfo, tool string, meta mcpCallMeta) platform.Member {
	caller := runtimeprotocol.ReadAICaller(m)
	if caller == nil {
		caller = &runtimeprotocol.AICaller{
			Kind:    "mcp",
			Name:    cmp.Or(strings.TrimSpace(client.Name), "mcp-client"),
			Version: strings.TrimSpace(client.Version),
		}
	} else {
		if caller.Name == "" {
			caller.Name = strings.TrimSpace(client.Name)
		}
		if caller.Version == "" {
			caller.Version = strings.TrimSpace(client.Version)
		}
	}
	if caller.Tool == "" && tool != "" {
		caller.Tool = strings.TrimSpace(tool)
	}
	if meta.Model != "" {
		caller.Model = strings.TrimSpace(meta.Model)
	}
	if meta.Event != "" && caller.Event == "" {
		caller.Event = strings.TrimSpace(meta.Event)
	}
	if meta.Run != "" && caller.Run == "" {
		caller.Run = strings.TrimSpace(meta.Run)
	}
	if meta.Usage.Input > 0 {
		caller.Input = meta.Usage.Input
	}
	if meta.Usage.Output > 0 {
		caller.Output = meta.Usage.Output
	}
	if meta.Usage.Cached > 0 {
		caller.Cached = meta.Usage.Cached
	}
	if meta.Usage.Cost > 0 {
		caller.Cost = meta.Usage.Cost
	}
	return runtimeprotocol.WithAICaller(m, caller)
}

func requestOrigin(r *http.Request) string {
	if r == nil || strings.TrimSpace(r.Host) == "" {
		return ""
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + strings.TrimSpace(r.Host)
}

func rpcError(id json.RawMessage, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}

func toolName(raw string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range raw {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
			lastUnderscore = false
		} else if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	clean := strings.Trim(b.String(), "_")
	if len(clean) <= mcpToolNameLimit {
		return clean
	}
	sum := sha256.Sum256([]byte(raw))
	return clean[:mcpToolNameLimit-9] + "_" + hex.EncodeToString(sum[:4])
}

type generatedTool struct {
	spec       map[string]any
	kind       string
	objectInfo platform.RecordTypeInfo
	action     platform.Action
	payloadMap map[string]any
}

func (g Tools) compile(m platform.Member) (runtimeprotocol.ModelToolCatalog, map[string]generatedTool, []string) {
	m.AIContext = true
	surface := g.Host.ModelToolCatalog(m)
	byName := map[string]generatedTool{}
	var order []string
	entities := map[string]platform.RecordTypeInfo{}
	for _, info := range surface.Entities {
		entities[info.Type] = info
	}
	sortedEntities := slices.Clone(surface.Entities)
	slices.SortFunc(sortedEntities, func(a, b platform.RecordTypeInfo) int { return strings.Compare(a.Type, b.Type) })
	for _, info := range sortedEntities {
		name := toolName("query_" + info.Type)
		spec := buildQueryToolSpec(name, info)
		byName[name] = generatedTool{spec: spec, kind: "query", objectInfo: info}
		order = append(order, name)
	}
	sortedActions := slices.Clone(surface.Actions)
	slices.SortFunc(sortedActions, func(a, b platform.Action) int { return strings.Compare(a.Schema, b.Schema) })
	for _, action := range sortedActions {
		if excludedMCPAction(action) {
			continue
		}
		name := toolName(action.Schema)
		if _, collision := byName[name]; collision {
			name = toolName("act_" + action.Schema)
		}
		var entityPtr *platform.RecordTypeInfo
		if info, ok := entities[action.Target]; ok {
			cp := info
			entityPtr = &cp
		}
		hidden := surface.HiddenAIFields[action.Target]
		filtered := action
		if len(hidden) > 0 {
			filtered.Payload = slices.DeleteFunc(slices.Clone(action.Payload), func(f platform.Field) bool {
				return slices.Contains(hidden, f.Name)
			})
		}
		spec, payloadProps := buildActionToolSpec(name, filtered, entityPtr, m.RolesIn(action.Target))
		byName[name] = generatedTool{spec: spec, kind: "action", objectInfo: valueOrZero(entityPtr), action: action, payloadMap: payloadProps}
		order = append(order, name)
	}
	return surface, byName, order
}

func valueOrZero(p *platform.RecordTypeInfo) platform.RecordTypeInfo {
	if p == nil {
		return platform.RecordTypeInfo{}
	}
	return *p
}

func excludedMCPAction(a platform.Action) bool {
	return a.Automation || a.ConfirmationOnly ||
		a.Schema == "work.question.answer" ||
		a.Schema == runtimeprotocol.WorkQuestionRequestSchema ||
		strings.HasPrefix(a.Schema, "agent.run.")
}

func buildQueryToolSpec(name string, info platform.RecordTypeInfo) map[string]any {
	label := cmp.Or(info.Plural, info.Title, info.Type)
	desc := fmt.Sprintf("Query %s (%s).", label, info.Type)
	if strings.TrimSpace(info.Description) != "" {
		desc += " " + strings.TrimSpace(info.Description)
	}
	if fields := describeReadableFields(info); fields != "" {
		desc += " Readable fields: " + fields + "."
	}
	props := map[string]any{
		"id":       map[string]any{"type": "string", "description": "Optional record ID to fetch one record"},
		"domain":   map[string]any{"type": "array", "items": map[string]any{"type": "array"}, "description": "Optional ObjectSet filter clauses, e.g. [[\"stage\",\"=\",\"new\"]]"},
		"search":   map[string]any{"type": "string", "description": "Optional full-text search query"},
		"sort":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional sort fields; prefix with - for descending"},
		"limit":    map[string]any{"type": "integer", "description": "Maximum records to return (1..50)"},
		"offset":   map[string]any{"type": "integer", "description": "Records to skip"},
		"groups":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional fields to group by when aggregating"},
		"measures": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional aggregate measures: count, sum:field, avg:field, min:field, max:field"},
	}
	return map[string]any{
		"name":        name,
		"title":       "Query " + label,
		"description": desc,
		"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false},
		"inputSchema": map[string]any{"type": "object", "properties": props, "required": []string{}},
	}
}

func describeReadableFields(info platform.RecordTypeInfo) string {
	limit := min(len(info.Fields), mcpMaxFieldsInDesc)
	parts := make([]string, 0, limit)
	for _, f := range info.Fields[:limit] {
		part := f.Name + " (" + f.Type
		if len(f.Choices) > 0 {
			part += ": " + strings.Join(f.Choices, "|")
		}
		if f.Ref != "" {
			part += " -> " + f.Ref
		}
		parts = append(parts, part+")")
	}
	return strings.Join(parts, ", ")
}

func buildActionToolSpec(name string, action platform.Action, entity *platform.RecordTypeInfo, roles []string) (map[string]any, map[string]any) {
	desc := strings.TrimSpace(action.Title)
	if help := strings.TrimSpace(action.Description); help != "" {
		if desc != "" {
			desc += ": " + help
		} else {
			desc = help
		}
	}
	risk := "direct"
	if action.Ask {
		risk = "ask"
		if strings.TrimSpace(action.AskReason) != "" {
			desc += " Ask first (holds a question in the member's inbox until approved: " + strings.TrimSpace(action.AskReason) + ")."
		} else {
			desc += " Ask first (holds a question in the member's inbox until approved)."
		}
	} else if action.NeedsApproval {
		desc += " Requires workflow approval."
	}
	payloadSchema := platform.ActionInputSchema(action, entity, roles)
	payloadProps, _ := payloadSchema["properties"].(map[string]any)
	if payloadProps == nil {
		payloadProps = map[string]any{}
	}
	payloadReq, _ := payloadSchema["required"].([]string)
	targetDesc := "The ID of the " + cmp.Or(action.Target, "record") + " this action targets"
	if action.New {
		targetDesc = "Optional ID for the new " + cmp.Or(action.Target, "record") + "; omit to assign automatically"
	}
	props := map[string]any{
		"target":           map[string]any{"type": "string", "description": targetDesc},
		"payload":          payloadSchema,
		"expectedRevision": map[string]any{"type": "integer", "description": "Optional expected revision for optimistic concurrency"},
		"idempotencyKey":   map[string]any{"type": "string", "description": "Optional idempotency key when retrying the exact call"},
	}
	var required []string
	if !action.New && action.Target != "" {
		required = append(required, "target")
	}
	if len(payloadReq) > 0 {
		required = append(required, "payload")
	}
	if required == nil {
		required = []string{}
	}
	spec := map[string]any{
		"name":            name,
		"title":           action.Title,
		"description":     desc,
		"x-platform-risk": risk,
		"annotations":     map[string]any{"readOnlyHint": false, "destructiveHint": action.Ask, "openWorldHint": false},
		"inputSchema":     map[string]any{"type": "object", "properties": props, "required": required},
	}
	if action.Ask && action.AskReason != "" {
		spec["x-platform-ask-reason"] = action.AskReason
	}
	return spec, payloadProps
}

func (g Tools) List(m platform.Member) []map[string]any {
	_, byName, order := g.compile(m)
	out := make([]map[string]any, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name].spec)
	}
	return out
}

func (g Tools) Call(ctx context.Context, origin string, m platform.Member, name string, args map[string]any) map[string]any {
	answer := func(v map[string]any, isError bool) map[string]any {
		raw, _ := json.Marshal(v)
		res := map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(raw)}},
			"isError": isError,
		}
		if !isError {
			res["structuredContent"] = v
		}
		return res
	}
	surface, byName, _ := g.compile(m)
	tool, ok := byName[name]
	if !ok {
		return answer(map[string]any{"error": "tool is not in your authorized catalog", "tool": name}, true)
	}
	if args == nil {
		args = map[string]any{}
	}
	if tool.kind == "query" {
		q, singleID, problem := parseObjectQuery(tool.objectInfo.Type, args)
		if problem != "" {
			return answer(map[string]any{"error": problem}, true)
		}
		raw, err := g.Host.QueryModelObjects(m, q, g.Now())
		if err != nil {
			return answer(map[string]any{"error": cmp.Or(err.Message, err.Error()), "code": err.Code.String()}, true)
		}
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		body["object"] = tool.objectInfo.Type
		body["link"] = workspaceURL(origin, surface.Applications, tool.objectInfo.Type, singleID)
		return answer(body, false)
	}
	key, _ := args["idempotencyKey"].(string)
	key = strings.TrimSpace(key)
	if key == "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		key = "mcp:" + base64.RawURLEncoding.EncodeToString(b)
	}
	target, _ := args["target"].(string)
	target = strings.TrimSpace(target)
	if target == "" && tool.action.New && tool.objectInfo.Naming == "" {
		sum := sha256.Sum256([]byte(g.Host.TenantID() + "/" + tool.action.Target + "/" + key))
		target = "mcp-" + hex.EncodeToString(sum[:6])
	}
	payload := extractActionPayload(args, tool.payloadMap)
	var expectedRev *uint32
	if rev, ok := parseUint32Arg(args["expectedRevision"]); ok {
		expectedRev = &rev
	}
	now := g.Now()
	sub := &pb.Submission{
		TenantId:         g.Host.TenantID(),
		PrincipalId:      m.ID,
		Target:           &pb.EntityRef{Type: tool.action.Target, Id: target},
		Schema:           &pb.SchemaRef{Name: tool.action.Schema, Version: 1},
		IdempotencyKey:   key,
		Payload:          platform.Raw(payload),
		ExpectedRevision: expectedRev,
	}
	accepted, err, issues := g.Host.SubmitModelAction(ctx, m, sub, now)
	if err != nil || accepted == nil {
		errOut := map[string]any{"error": "action failed"}
		if err != nil {
			errOut = map[string]any{"error": cmp.Or(err.Message, err.Error()), "code": err.Code.String()}
		}
		if len(issues) > 0 {
			errOut["issues"] = issues
		}
		return answer(errOut, true)
	}
	if accepted.GetSubmission().GetTarget().GetType() == runtimeprotocol.WorkQuestionType && accepted.GetSubmission().GetSchema().GetName() == runtimeprotocol.WorkQuestionRequestSchema {
		qid := accepted.GetSubmission().GetTarget().GetId()
		return answer(map[string]any{
			"status":     "pending_approval",
			"ask":        true,
			"action":     tool.action.Schema,
			"questionId": qid,
			"reason":     tool.action.AskReason,
			"changeId":   accepted.GetChangeId(),
			"target":     map[string]any{"object": tool.action.Target, "id": target},
			"link":       origin + "/home/inbox/approvals?question=" + url.QueryEscape(qid),
			"targetLink": workspaceURL(origin, surface.Applications, tool.action.Target, target),
		}, false)
	}
	actualID := cmp.Or(accepted.GetSubmission().GetTarget().GetId(), target)
	res := map[string]any{
		"status":   "accepted",
		"action":   tool.action.Schema,
		"changeId": accepted.GetChangeId(),
		"target":   map[string]any{"object": tool.action.Target, "id": actualID},
		"link":     workspaceURL(origin, surface.Applications, tool.action.Target, actualID),
	}
	if tool.action.Target != "" && actualID != "" {
		rawPage, readErr := g.Host.QueryModelObjects(m, runtimeprotocol.ModelObjectQuery{
			Set:   platform.ObjectSet{Object: tool.action.Target, Domain: platform.Raw([][]any{{"id", "=", actualID}})},
			Limit: 1,
		}, now)
		if readErr == nil && len(rawPage) > 0 {
			var page struct {
				Records []json.RawMessage `json:"records"`
			}
			if json.Unmarshal(rawPage, &page) == nil && len(page.Records) > 0 {
				var rec any
				if json.Unmarshal(page.Records[0], &rec) == nil && rec != nil {
					res["record"] = rec
				}
			}
		}
	}
	return answer(res, false)
}

func extractActionPayload(args map[string]any, payloadProps map[string]any) map[string]any {
	if nested, ok := args["payload"].(map[string]any); ok {
		return maps.Clone(nested)
	}
	out := map[string]any{}
	reserved := map[string]bool{"target": true, "payload": true, "expectedRevision": true, "idempotencyKey": true}
	for k, v := range args {
		if reserved[k] {
			continue
		}
		if len(payloadProps) == 0 || payloadProps[k] != nil {
			out[k] = v
		}
	}
	return out
}

func parseObjectQuery(object string, args map[string]any) (runtimeprotocol.ModelObjectQuery, string, string) {
	var q runtimeprotocol.ModelObjectQuery
	q.Set.Object = object
	if search, ok := args["search"].(string); ok {
		q.Set.Search = strings.TrimSpace(search)
	}
	var clauses []any
	if rawDomain, ok := args["domain"]; ok && rawDomain != nil {
		slice, isSlice := rawDomain.([]any)
		if !isSlice {
			return q, "", "domain must be an array of filter clauses"
		}
		clauses = slices.Clone(slice)
	}
	singleID, _ := args["id"].(string)
	singleID = strings.TrimSpace(singleID)
	if singleID != "" {
		if len(clauses) > 0 {
			clauses = append([]any{"&", []any{"id", "=", singleID}}, clauses...)
		} else {
			clauses = []any{[]any{"id", "=", singleID}}
		}
	}
	if len(clauses) > 0 {
		q.Set.Domain = platform.Raw(clauses)
	}
	q.Sort = parseStringSlice(args["sort"])
	q.Groups = parseStringSlice(args["groups"])
	q.Measures = parseStringSlice(args["measures"])
	if limit, ok := parseIntArg(args["limit"]); ok {
		if limit < 0 {
			return q, "", "limit must not be negative"
		}
		q.Limit = limit
	}
	if offset, ok := parseIntArg(args["offset"]); ok {
		if offset < 0 {
			return q, "", "offset must not be negative"
		}
		q.Offset = offset
	}
	if len(q.Groups) == 0 && len(q.Measures) == 0 {
		if q.Limit <= 0 || q.Limit > 50 {
			q.Limit = 50
		}
	}
	if err := q.Set.Check(); err != nil {
		return q, "", err.Error()
	}
	return q, singleID, ""
}

func parseStringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		if s, ok := v.([]string); ok {
			return slices.Clone(s)
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func parseIntArg(v any) (int, bool) {
	switch n := v.(type) {
	case nil:
		return 0, false
	case int:
		return n, true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return i, err == nil
	default:
		return 0, false
	}
}

func parseUint32Arg(v any) (uint32, bool) {
	i, ok := parseIntArg(v)
	if !ok || i < 0 {
		return 0, false
	}
	return uint32(i), true
}

func workspaceURL(origin string, apps []runtimeprotocol.ApplicationView, objectType, recordID string) string {
	path := "/ontology/objects/" + url.PathEscape(objectType)
	for _, view := range apps {
		appSeg := cmp.Or(view.Name, view.App)
		found := false
		for _, group := range view.Menu {
			for _, page := range group.Entries {
				if page.Object == objectType && appSeg != "" && page.Name != "" {
					path = "/" + url.PathEscape(appSeg) + "/" + url.PathEscape(page.Name)
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			break
		}
	}
	if strings.TrimSpace(recordID) != "" {
		path += "/" + url.PathEscape(strings.TrimSpace(recordID))
	}
	return origin + path
}
