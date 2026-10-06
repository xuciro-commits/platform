package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/flow"
	"platformserver/platform"
)

// ApplicationRun is a projection of original authorized runtime reads. Its
// relationship describes resource use, never exclusive ownership by an app.
type ApplicationRun struct {
	ID           string             `json:"id"`
	Kind         string             `json:"kind"`
	Title        string             `json:"title"`
	Resource     *platform.AssetRef `json:"resource,omitempty"`
	ResourceName string             `json:"resourceName"`
	State        string             `json:"state"`
	Version      string             `json:"version,omitempty"`
	Release      string             `json:"release,omitempty"`
	Dependencies string             `json:"dependencies,omitempty"`
	Module       string             `json:"module,omitempty"`
	Node         string             `json:"node,omitempty"`
	Error        string             `json:"error,omitempty"`
	Association  string             `json:"association"`
	Shared       []string           `json:"shared"`
	Created      time.Time          `json:"created"`
}
type ApplicationRunPage struct {
	Application platform.AssetRef `json:"application"`
	Title       string            `json:"title"`
	Runs        []ApplicationRun  `json:"runs"`
	Total       int               `json:"total"`
}

func (t *Tenant) ApplicationRuns(m platform.Member, ref platform.AssetRef, offset, limit int, now time.Time) (ApplicationRunPage, *kernel.Error) {
	out := ApplicationRunPage{Application: ref, Runs: []ApplicationRun{}}
	if ref.Kind != platform.AssetApp || ref.Check() != nil || offset < 0 || limit < 1 || limit > 100 {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an application and a run page of 1–100")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.admits(m); err != nil {
		return out, err
	}
	visible := t.Definitions(m)
	var app *platform.Definition
	for _, d := range visible {
		if d.Ref == ref && d.Application != nil {
			copy := d
			app = &copy
			break
		}
	}
	if app == nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Application is unavailable to this member")
	}
	out.Title = app.Application.Title
	// Current and retained membership are indexed independently. Removing a
	// resource today must not hide a run started with an earlier application.
	current, graphs, graphErr := t.applicationRunGraphsLocked(ref)
	if graphErr != nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
	}
	graphFor := func(release string) map[platform.AssetRef]bool {
		if g, ok := graphs[release]; ok {
			return g
		}
		return graphs[""]
	}
	union := map[platform.AssetRef]bool{}
	for _, g := range graphs {
		for r := range g {
			union[r] = true
		}
	}
	shares := func(resource platform.AssetRef) []string {
		names := []string{}
		for _, d := range visible {
			if d.Ref != ref && d.Application != nil && applicationRunGraph(d.Ref, current)[resource] {
				names = append(names, d.Application.Title)
			}
		}
		slices.Sort(names)
		return slices.Compact(names)
	}
	appendRun := func(run ApplicationRun) {
		run.Shared = []string{}
		if run.Resource != nil {
			run.Shared = shares(*run.Resource)
		}
		out.Runs = append(out.Runs, run)
	}
	records := func(typ string, domain json.RawMessage) ([]any, *kernel.Error) {
		rows := []any{}
		for skip := 0; ; skip += 500 {
			page, err := t.Records(m, typ, platform.Query{Domain: domain, Sort: []string{"-created"}, Offset: skip, Limit: 500, Archived: true}, now)
			if err != nil {
				if err.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED || err.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND {
					return rows, nil
				}
				return nil, err
			}
			if page.Total > 10000 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Application run inventory exceeds its 10000-record profile")
			}
			rows = append(rows, page.Records...)
			if skip+len(page.Records) >= page.Total {
				return rows, nil
			}
			if len(page.Records) == 0 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Run inventory changed during reading")
			}
		}
	}
	flows := []string{}
	for r := range union {
		if r.Kind == platform.AssetFlow {
			flows = append(flows, r.Name)
		}
	}
	slices.Sort(flows)
	relatedFlows := map[string]ApplicationRun{}
	operationNodes := map[string]string{}
	if len(flows) > 0 {
		domain, _ := json.Marshal([]any{[]any{"flow", "in", flows}})
		rows, err := records(flow.InstanceType, domain)
		if err != nil {
			return out, err
		}
		for _, row := range rows {
			var x flow.FlowInstance
			raw, _ := json.Marshal(row)
			if json.Unmarshal(raw, &x) != nil {
				continue
			}
			var resource platform.AssetRef
			for r := range graphFor(x.Release) {
				if r.Kind == platform.AssetFlow && r.Name == x.Flow {
					resource = r
					break
				}
			}
			if resource.App == "" {
				continue
			}
			run := ApplicationRun{ID: x.ID, Kind: "flow", Title: x.Title, Resource: &resource, ResourceName: x.Flow, State: x.State, Version: fmt.Sprint(x.Version), Release: x.Release, Dependencies: x.Dependencies, Association: "resource", Created: x.Created.At}
			if !x.Withheld {
				for _, token := range x.Tokens {
					if token.Operation != "" {
						operationNodes[token.Operation] = token.Step
					}
					if token.Error != "" {
						run.Node, run.Error = token.Step, token.Error
						break
					}
				}
			}
			if !x.Withheld && run.Error == "" {
				for i := len(x.Trace) - 1; i >= 0; i-- {
					line := x.Trace[i]
					if slices.Contains([]string{"retry", "fault", "stuck", "compensating"}, line.What) && line.Detail != "" {
						run.Node, run.Error = line.Step, line.Detail
						break
					}
				}
			}
			relatedFlows[x.ID] = run
			appendRun(run)
		}
	}
	if rows, err := records(RunType, nil); err != nil {
		return out, err
	} else {
		for _, row := range rows {
			var x AgentRunRecord
			raw, _ := json.Marshal(row)
			if json.Unmarshal(raw, &x) != nil {
				continue
			}
			association := ""
			release := x.Release
			if parent, ok := relatedFlows[x.Flow]; ok {
				association = "flow"
				if release == "" {
					release = parent.Release
				}
			} else if typ, _, ok := strings.Cut(x.Ref, "/"); ok {
				for r := range graphFor(release) {
					if r.Kind == platform.AssetObject && r.Name == typ {
						association = "subject"
						break
					}
				}
			}
			if association == "" {
				continue
			}
			run := ApplicationRun{ID: x.ID, Kind: "agent", Title: x.Title, ResourceName: x.Agent, State: x.State, Version: x.DefinitionVersion, Release: release, Association: association, Node: x.Step, Created: x.Created.At}
			if !x.Withheld {
				run.Error = x.Stopped
			}
			appendRun(run)
		}
	}
	// Original operationResult rechecks the initiating member and every source;
	// holding a console role alone does not grant another person's call output.
	t.opsMu.Lock()
	effects := make([]platform.Effect, 0, len(t.outbound))
	for _, x := range t.outbound {
		if x.Endpoint == operationEndpoint {
			effects = append(effects, x.Effect)
		}
	}
	t.opsMu.Unlock()
	for _, effect := range effects {
		var binding operationBinding
		if json.Unmarshal([]byte(effect.Body), &binding) != nil || binding.Build != nil {
			continue
		}
		result, err := t.ReadOperation(m, effect.ID)
		if err != nil {
			continue
		}
		resource := platform.AssetRef{App: effect.App, Kind: platform.AssetCompute, Name: binding.Definition.Name}
		association := "resource"
		parentType, parentID, _ := strings.Cut(effect.Target, "/")
		_, hasParent := relatedFlows[parentID]
		if parentType == flow.InstanceType && hasParent {
			association = "flow"
		}
		if !graphFor(binding.Call.Release)[resource] && association != "flow" {
			association = ""
			for _, source := range binding.Call.Sources {
				typ, id, _ := strings.Cut(source, "/")
				if typ == flow.InstanceType {
					if _, ok := relatedFlows[id]; ok {
						association = "flow"
						break
					}
				}
			}
			if association == "" {
				continue
			}
		}
		appendRun(ApplicationRun{ID: effect.ID, Kind: "compute", Title: binding.Definition.Title, Resource: &resource, ResourceName: resource.Name, State: result.State, Version: operationStartupVersion(binding.Call), Release: binding.Call.Release, Dependencies: binding.Call.Dependencies, Module: binding.Call.Module, Node: operationNodes[effect.ID], Error: result.Error, Association: association, Created: effect.At})
	}
	slices.SortFunc(out.Runs, func(a, b ApplicationRun) int {
		if a.Created.Before(b.Created) {
			return 1
		}
		if a.Created.After(b.Created) {
			return -1
		}
		return strings.Compare(a.Kind+"/"+a.ID, b.Kind+"/"+b.ID)
	})
	out.Total = len(out.Runs)
	start := min(offset, out.Total)
	out.Runs = out.Runs[start:min(start+limit, out.Total)]
	return out, nil
}

func applicationRunGraph(root platform.AssetRef, assets map[platform.AssetRef]platform.ReleaseAsset) map[platform.AssetRef]bool {
	graph := map[platform.AssetRef]bool{}
	var visit func(platform.AssetRef)
	visit = func(ref platform.AssetRef) {
		if graph[ref] {
			return
		}
		graph[ref] = true
		asset, ok := assets[ref]
		if !ok {
			return
		}
		for _, dependency := range asset.Requires {
			visit(dependency)
		}
		if ref.Kind == platform.AssetApp {
			var app platform.Application
			if json.Unmarshal(asset.Body, &app) == nil {
				for _, dependency := range app.Dependencies(ref.App) {
					visit(dependency)
				}
			}
		}
	}
	visit(root)
	return graph
}

// The canonical release owner supplies published flow/compute dependencies;
// the runtime view does not reconstruct them from menu labels or draft data.
func (t *Tenant) applicationRunGraphsLocked(root platform.AssetRef) (map[platform.AssetRef]platform.ReleaseAsset, map[string]map[platform.AssetRef]bool, error) {
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return nil, nil, err
	}
	current := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, a := range available {
		current[a.Ref] = a
	}
	graphs := map[string]map[platform.AssetRef]bool{"": applicationRunGraph(root, current)}
	for id, raw := range t.releaseCandidates {
		candidate, err := platform.ReadCandidate(id, raw)
		if err != nil {
			return nil, nil, err
		}
		assets := map[platform.AssetRef]platform.ReleaseAsset{}
		for _, a := range candidate.Assets {
			assets[a.Ref] = a
		}
		if _, ok := assets[root]; ok {
			graphs[id] = applicationRunGraph(root, assets)
		}
	}
	return current, graphs, nil
}

func operationStartupVersion(call platform.OperationCall) string {
	if call.Version > 0 {
		return fmt.Sprint(call.Version)
	}
	if call.OwnerVersion != "" {
		return "code:" + call.OwnerVersion
	}
	return ""
}
