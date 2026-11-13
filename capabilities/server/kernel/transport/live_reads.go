package transport

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"platformserver/platform"
)

type LiveVersions struct {
	Lock          func() func()
	Generation    func() uint64
	LiveVersion   func(typ string) uint64
	Scoped        func(typ string) (exists, scoped bool)
	ComputeOwners func(platform.AssetRef) ([]string, error)
	ReadOwner     func(name string) string
	BranchType    string
}

func LiveReadAllowed(p string) bool {
	if strings.HasPrefix(p, "/v1/packages/") && (strings.HasSuffix(p, "/preparation") || strings.HasSuffix(p, "/ownership")) {
		return true
	}
	if strings.HasPrefix(p, "/v1/queries/") || strings.HasPrefix(p, "/v1/object-sets/") {
		return true
	}
	if p == "/v1/page-model/read" {
		return true
	}
	if strings.HasPrefix(p, "/v1/applications/") && (strings.HasSuffix(p, "/runs") || strings.HasSuffix(p, "/runtime") || strings.HasSuffix(p, "/diagnostic")) {
		return true
	}
	if strings.HasPrefix(p, "/v1/dispatches/") && strings.HasSuffix(p, "/targets") {
		return true
	}
	if strings.HasPrefix(p, "/v1/capabilities/") || strings.HasPrefix(p, "/v1/releases/candidates/") {
		return true
	}
	if strings.HasPrefix(p, "/v1/records/") || strings.HasPrefix(p, "/v1/chain/") || p == "/v1/branches" || strings.HasPrefix(p, "/v1/branches/") || p == "/v1/resources" || strings.HasPrefix(p, "/v1/resources/") || strings.HasPrefix(p, "/v1/working-state/") {
		return true
	}
	if rest, ok := strings.CutPrefix(p, "/v1/"); ok && rest != "" && !strings.Contains(rest, "/") {
		return !slices.Contains([]string{"changes", "sign-in", "openapi.json"}, rest)
	}
	return slices.Contains([]string{"/v1/authz/explain", "/v1/ai/model-selection", "/v1/ai/knowledge-index", "/v1/ai/vendors", "/v1/releases/active", "/v1/releases/candidates", "/v1/releases/drafts/working", "/v1/build/drafts/actions", "/v1/page-model/rules"}, p)
}
func liveOperationRead(p string) bool {
	return strings.HasPrefix(p, "/v1/applications/") && (strings.HasSuffix(p, "/runs") || strings.HasSuffix(p, "/runtime") || strings.HasSuffix(p, "/diagnostic")) || strings.HasPrefix(p, "/v1/dispatches/") && strings.HasSuffix(p, "/targets") || strings.HasPrefix(p, "/v1/capabilities/calls/compute/") || slices.Contains([]string{"/v1/health", "/v1/work", "/v1/deliveries", "/v1/effects", "/v1/integration-effects", "/v1/endpoints", "/v1/connectors", "/v1/protocols", "/v1/agent-overview", "/v1/ai-usage", "/v1/ai/knowledge-index"}, p)
}

func (v LiveVersions) Of(query string, global, meta, seq, published int64, owners, ops map[string]int64) string {
	target, body, _ := strings.Cut(query, "\n")
	u, _ := url.Parse(target)
	p := u.Path
	if p == "/v1/page-model/read" {
		return fmt.Sprintf("%d/%d/%d", global, meta, v.Generation())
	}
	if strings.HasPrefix(p, "/v1/releases/candidates/") {
		return fmt.Sprintf("%d/%d/%d/%d", global, meta, owners["ai"], owners["agent"])
	}
	if p == "/v1/ai/model-selection" {
		return fmt.Sprintf("%d/%d/%d/%d", global, meta, owners["ai"], owners["agent"])
	}
	if strings.HasPrefix(p, "/v1/packages/") && (strings.HasSuffix(p, "/preparation") || strings.HasSuffix(p, "/ownership")) {
		return fmt.Sprintf("%d/%d/%d/%d", global, meta, owners["ai"], owners["agent"])
	}
	if strings.HasPrefix(p, "/v1/queries/") {
		unlock := v.Lock()
		generation := v.Generation()
		unlock()
		return fmt.Sprintf("%d/%d/%d", global, meta, generation)
	}
	if strings.HasPrefix(p, "/v1/applications/") && (strings.HasSuffix(p, "/runs") || strings.HasSuffix(p, "/runtime") || strings.HasSuffix(p, "/diagnostic")) {
		parts := strings.Split(p, "/")
		if len(parts) != 6 {
			return fmt.Sprintf("%d/%d", seq, published)
		}
		ref := platform.AssetRef{App: parts[3], Kind: platform.AssetApp, Name: parts[4]}
		relevant, err := v.ComputeOwners(ref)
		if err != nil {
			return fmt.Sprint(global, meta, seq)
		}
		slices.Sort(relevant)
		relevant = slices.Compact(relevant)
		operationVersion := ops[""]
		for _, owner := range relevant {
			operationVersion += ops[owner]
		}
		unlock := v.Lock()
		a, b := v.LiveVersion("flow.instance"), v.LiveVersion("agent.run")
		unlock()
		return fmt.Sprintf("%d/%d/%d/%d/%d/%d", global, meta, a, b, operationVersion, published)
	}
	if liveOperationRead(p) || strings.HasPrefix(p, "/v1/chain/") || p == "/v1/business-refusals" || p == "/v1/notifications" || p == "/v1/audit" || p == "/v1/inbox" || p == "/v1/personal-reads" {
		return fmt.Sprintf("%d/%d", seq, published)
	}
	if strings.HasPrefix(p, "/v1/object-sets/") {
		var request struct {
			Set        platform.ObjectSet     `json:"set"`
			Collection *platform.RecordSetRef `json:"collection,omitempty"`
		}
		typ := ""
		var walk func(platform.ObjectSet) bool
		walk = func(set platform.ObjectSet) bool {
			if set.Interface != "" || set.Around != nil || set.Object != "" && typ != "" && set.Object != typ {
				return false
			}
			if set.Object != "" {
				typ = set.Object
			}
			_, operands := set.Operation()
			for _, operand := range operands {
				if !walk(operand) {
					return false
				}
			}
			return true
		}
		defer v.Lock()()
		version := v.Generation()
		if json.Unmarshal([]byte(body), &request) == nil && walk(request.Set) && request.Collection == nil {
			if exists, scoped := v.Scoped(typ); exists && !scoped {
				version = v.LiveVersion(typ)
			}
		}
		return fmt.Sprintf("%d/%d/%d", global, meta, version)
	}
	if p == "/v1/branches" || strings.HasPrefix(p, "/v1/branches/") || p == "/v1/resources" || strings.HasPrefix(p, "/v1/resources/") || strings.HasPrefix(p, "/v1/working-state/") {
		defer v.Lock()()
		if strings.HasSuffix(p, "/changes") {
			return fmt.Sprintf("%d/%d/%d", global, meta, v.LiveVersion(v.BranchType))
		}
		return fmt.Sprintf("%d/%d/%d", global, meta, v.Generation())
	}
	if rest, ok := strings.CutPrefix(p, "/v1/records/"); ok {
		typ, tail, _ := strings.Cut(rest, "/")
		defer v.Lock()()
		version := v.LiveVersion(typ)
		if exists, scoped := v.Scoped(typ); exists && (scoped || tail != "") {
			version = v.Generation()
		}
		return fmt.Sprintf("%d/%d/%d", global, meta, version)
	}
	if p == "/v1/enterprise" || p == "/v1/enterprise-references" || p == "/v1/enterprise-usage" {
		unlock := v.Lock()
		generation := v.Generation()
		unlock()
		return fmt.Sprintf("%d/%d/%d/%d", global, meta, owners["enterprise"], generation)
	}
	owner := "platform"
	if name, ok := strings.CutPrefix(p, "/v1/"); ok && !strings.Contains(name, "/") {
		if app := v.ReadOwner(name); app != "" {
			owner = app
		}
	}
	switch p {
	case "/v1/releases/active", "/v1/releases/candidates", "/v1/releases/drafts/working", "/v1/build/drafts/actions", "/v1/release-profile", "/v1/capabilities", "/v1/page-model/rules":
		owner = "build"
	case "/v1/ai-limits", "/v1/ai-models", "/v1/ai-providers", "/v1/ai/vendors":
		owner = "ai"
	case "/v1/organization", "/v1/enterprise", "/v1/enterprise-metamodel", "/v1/enterprise-published", "/v1/enterprise-references", "/v1/enterprise-usage",
		"/v1/enterprise-query", "/v1/enterprise-resolve":
		owner = "enterprise"
	case "/v1/flows":
		owner = "flow"
	case "/v1/agents", "/v1/runs", "/v1/memories":
		owner = "agent"
	case "/v1/me", "/v1/actions", "/v1/entities", "/v1/definitions", "/v1/apps", "/v1/protocols":
		return fmt.Sprintf("%d/%d", global, meta)
	}
	return fmt.Sprintf("%d/%d/%d", global, meta, owners[owner])
}
