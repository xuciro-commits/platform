package applications

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func (ctx Context) Runs(m platform.Member, ref platform.AssetRef, offset, limit int, now time.Time, filters ...platform.ApplicationRunFilter) (platform.ApplicationRunPage, *kernel.Error) {
	out := platform.ApplicationRunPage{Application: ref, Runs: []platform.ApplicationRun{}}
	if ref.Kind != platform.AssetApp || ref.Check() != nil || offset < 0 || limit < 1 || limit > 100 {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an application and a run page of 1–100")
	}
	filter := platform.ApplicationRunFilter{}
	if len(filters) > 0 {
		filter = filters[0]
	}
	if filter.Kind != "" && !slices.Contains([]string{"flow", "agent", "compute", "function", "effect", "job", "dispatch"}, filter.Kind) {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a valid application run kind")
	}
	if len(filter.State) > 64 || len(filter.Subject) > 512 || !filter.Since.IsZero() && !filter.Until.IsZero() && filter.Since.After(filter.Until) {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a valid application run time range")
	}
	if err := ctx.Admit(m); err != nil {
		return out, err
	}
	visible := ctx.Definitions(m)
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
	current, graphs, graphErr := ctx.Graphs(ref)
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
			if d.Ref != ref && d.Application != nil && RunGraph(d.Ref, current)[resource] {
				names = append(names, d.Application.Title)
			}
		}
		slices.Sort(names)
		return slices.Compact(names)
	}
	belongs := func(bindings []platform.AssetBinding) bool {
		return slices.ContainsFunc(bindings, func(binding platform.AssetBinding) bool { return binding.Ref == ref })
	}
	visibleApps := map[platform.AssetRef]string{}
	for _, definition := range visible {
		if definition.Application != nil {
			visibleApps[definition.Ref] = definition.Application.Title
		}
	}
	appendRun := func(run platform.ApplicationRun) {
		if len(run.Applications) > 0 && !belongs(run.Applications) {
			return
		}
		if filter.Kind != "" && run.Kind != filter.Kind || filter.State != "" && run.State != filter.State || filter.Subject != "" && run.Subject != filter.Subject || !filter.Since.IsZero() && run.Created.Before(filter.Since) || !filter.Until.IsZero() && run.Created.After(filter.Until) {
			return
		}
		run.Applications = slices.DeleteFunc(slices.Clone(run.Applications), func(binding platform.AssetBinding) bool { return visibleApps[binding.Ref] == "" })
		if belongs(run.Applications) {
			run.Association = "application"
		}
		run.Shared = []string{}
		if len(run.Applications) > 0 {
			for _, binding := range run.Applications {
				if binding.Ref != ref {
					run.Shared = append(run.Shared, visibleApps[binding.Ref])
				}
			}
			slices.Sort(run.Shared)
		} else if run.Resource != nil {
			run.Shared = shares(*run.Resource)
		}
		out.Runs = append(out.Runs, run)
		if run.Kind == "agent" || run.Kind == "function" {
			out.Summary.AITokens += run.AITokens
			out.Summary.AIUSD += run.AIUSD
			if !run.CostReported {
				out.Summary.UnmeteredAI++
			}
		}
		if slices.Contains([]string{"failed", "stuck", "blocked", "stopped", "rejected"}, run.State) || run.Error != "" {
			out.Summary.Failed++
		}
		if (run.Kind == "effect" || run.Kind == "compute") && !slices.Contains([]string{"delivered", "rejected", "failed", "discarded", "compensated"}, run.State) {
			out.Summary.PendingEffects++
		}
		if run.Kind == "flow" && slices.Contains([]string{"running", "waiting", "paused"}, run.State) {
			out.Summary.WaitingFlows++
		}
	}
	records := func(typ string, domain json.RawMessage) ([]any, *kernel.Error) {
		rows, err := ctx.Rows(m, typ, platform.Query{Domain: domain, Archived: true}, now)
		if err != nil && (err.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED || err.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND) {
			return nil, nil
		}
		return rows, err
	}
	relatedFlows := map[string]platform.ApplicationRun{}
	readableFlows := map[string]bool{}
	operationNodes := map[string]string{}
	{
		rows, err := records(platform.FlowInstanceType, nil)
		if err != nil {
			return out, err
		}
		for _, row := range rows {
			var x platform.FlowInstance
			raw, _ := json.Marshal(row)
			if json.Unmarshal(raw, &x) != nil {
				continue
			}
			readableFlows[platform.FlowInstanceType+"/"+x.ID] = true
			var resource platform.AssetRef
			for r := range graphFor(x.Release) {
				if r.Kind == platform.AssetFlow && r.Name == x.Flow {
					resource = r
					break
				}
			}
			if resource.App == "" {
				if !belongs(x.Applications) {
					continue
				}
				owner, _, ok := strings.Cut(x.Flow, ".")
				if !ok {
					continue
				}
				resource = platform.AssetRef{App: owner, Kind: platform.AssetFlow, Name: x.Flow}
			}
			run := platform.ApplicationRun{Applications: x.Applications, Subject: x.Subject, ID: x.ID, Kind: "flow", Title: x.Title, Resource: &resource, ResourceName: x.Flow, State: x.State, Version: fmt.Sprint(x.Version), Release: x.Release, Dependencies: x.Dependencies, Association: "resource", Created: x.Created.At}
			if !x.Withheld {
				for _, token := range x.Tokens {
					if token.Operation != "" {
						operationNodes[token.Operation] = token.Step
					}
					if run.Error == "" && token.Error != "" && slices.Contains([]string{"retry", "undo", "stuck"}, token.Waits) {
						run.Node, run.Error = token.Step, token.Error
					}
				}
			}
			if !x.Withheld && run.Error == "" && (x.State == "compensating" || x.State == "compensated") {
				for i := len(x.Trace) - 1; i >= 0; i-- {
					line := x.Trace[i]
					if line.What == "compensating" && line.Detail != "" {
						run.Node, run.Error = line.Step, line.Detail
						break
					}
				}
			}
			relatedFlows[x.ID] = run
			appendRun(run)
		}
	}
	if rows, err := records(platform.RunType, nil); err != nil {
		return out, err
	} else {
		for _, row := range rows {
			var x platform.AgentRunRecord
			raw, _ := json.Marshal(row)
			if json.Unmarshal(raw, &x) != nil {
				continue
			}
			association := ""
			release := x.Release
			owner, name, named := strings.Cut(x.Agent, ".")
			resource := platform.AssetRef{App: owner, Kind: platform.AssetAgent, Name: name}
			if named && graphFor(release)[resource] {
				association = "resource"
			}
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
			if association == "" && !belongs(x.Applications) {
				continue
			}
			run := platform.ApplicationRun{Applications: x.Applications, Subject: x.Ref, ID: x.ID, Kind: "agent", Title: x.Title, Resource: &resource, ResourceName: x.Agent, State: x.State, Version: x.DefinitionVersion, Release: release, Association: association, Node: x.Step, Created: x.Created.At}
			if !x.Withheld {
				run.Error = x.Stopped
				run.AITokens, run.AIUSD = x.TokensUsed, x.Cost
			}
			appendRun(run)
		}
	}
	for _, computation := range ctx.Computations(m) {
		effect, binding, result := computation.Effect, computation, computation.Result
		resource := platform.AssetRef{App: effect.App, Kind: platform.AssetCompute, Name: binding.Ref.Name}
		association := "resource"
		subject := ""
		if readableFlows[effect.Target] {
			subject = effect.Target
		} else {
			for _, source := range binding.Call.Sources {
				if readableFlows[source] {
					subject = source
					break
				}
			}
		}
		parentType, parentID, _ := strings.Cut(effect.Target, "/")
		_, hasParent := relatedFlows[parentID]
		if parentType == platform.FlowInstanceType && hasParent {
			association = "flow"
		}
		if !belongs(effect.Applications) && !graphFor(binding.Call.Release)[resource] && association != "flow" {
			association = ""
			for _, source := range binding.Call.Sources {
				typ, id, _ := strings.Cut(source, "/")
				if typ == platform.FlowInstanceType {
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
		appendRun(platform.ApplicationRun{Applications: effect.Applications, Subject: subject, ID: effect.ID, Kind: "compute", Title: binding.Title, Resource: &resource, ResourceName: resource.Name, State: result.State, Version: operationStartupVersion(binding.Call), Release: binding.Call.Release, Dependencies: binding.Call.Dependencies, Module: binding.Call.Module, Node: operationNodes[effect.ID], Error: result.Error, Association: association, Created: effect.At})
	}
	if rows, err := records("build.function-call", nil); err != nil {
		return out, err
	} else {
		for _, row := range rows {
			var call functionRun
			raw, _ := json.Marshal(row)
			if json.Unmarshal(raw, &call) != nil {
				continue
			}
			owner := call.App
			if owner == "" {
				owner = "build"
			}
			resource := platform.AssetRef{App: owner, Kind: platform.AssetFunction, Name: call.Function}
			if !belongs(call.Applications) && !graphFor(call.Release)[resource] {
				continue
			}
			run := platform.ApplicationRun{Applications: call.Applications, ID: call.ID, Kind: "function", Title: call.Function, Resource: &resource, ResourceName: call.Function, State: call.State, Version: fmt.Sprint(call.Version), Release: call.Release, Dependencies: call.Dependencies, Association: "resource", Created: call.Created.At}
			if call.Source != "" {
				run.Subject = call.Contract.Object + "/" + call.Source
			}
			if !call.Withheld {
				run.Error = call.Reason
				run.AITokens, run.AIUSD, run.CostReported = call.InputTokens+call.OutputTokens, call.CostUSD, call.CostReported
			}
			appendRun(run)
		}
	}
	if rows, err := records(platform.DispatchType, nil); err != nil {
		return out, err
	} else {
		for _, row := range rows {
			var dispatch platform.DispatchRun
			if json.Unmarshal(platform.Raw(row), &dispatch) != nil {
				continue
			}
			resource := platform.AssetRef{App: "build", Kind: platform.AssetAction, Name: dispatch.Action}
			if !graphFor(dispatch.Release)[resource] {
				typ, _, _ := strings.Cut(dispatch.Subject, "/")
				resource = platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: typ}
				if !graphFor(dispatch.Release)[resource] {
					continue
				}
			}
			run := platform.ApplicationRun{ID: dispatch.ID, Kind: "dispatch", Title: dispatch.Command.Title, Subject: dispatch.Subject, Resource: &resource, ResourceName: resource.Name, State: dispatch.State, Release: dispatch.Release, Association: "subject", Created: dispatch.StartedAt}
			if !dispatch.Withheld && slices.Contains([]string{"unknown", "stuck", "failed"}, dispatch.State) {
				run.Error = dispatch.Reason
			}
			appendRun(run)
		}
	}
	if effects, err := ctx.Effects(m); err == nil {
		for _, effect := range effects {
			typ, _, _ := strings.Cut(effect.Target, "/")
			resource := platform.AssetRef{}
			for ref := range union {
				if ref.Kind == platform.AssetObject && ref.Name == typ {
					resource = ref
					break
				}
			}
			if resource.App == "" {
				if !belongs(effect.Applications) {
					continue
				}
				resource = platform.AssetRef{App: effect.App, Kind: platform.AssetApp, Name: effect.App}
			}
			run := platform.ApplicationRun{Applications: effect.Applications, ID: effect.ID, Kind: "effect", Title: effect.Event, Subject: effect.Target, Resource: &resource, ResourceName: resource.Name, State: effect.State, Association: "subject", Created: effect.At}
			if !effect.Withheld {
				run.Error = effect.Error
			}
			appendRun(run)
		}
	}
	if tasks, err := ctx.Jobs(m); err == nil {
		owners := map[string]bool{}
		for resource := range union {
			owners[resource.App] = true
		}
		for _, task := range tasks {
			if task.Kind != "job" || !belongs(task.Applications) && !owners[task.App] {
				continue
			}
			run := platform.ApplicationRun{Applications: task.Applications, ID: task.ID, Kind: "job", Title: task.Title, ResourceName: task.App, State: task.State, Association: "owner", Created: task.Last}
			if !task.Withheld {
				run.Error = task.Error
			}
			appendRun(run)
		}
	}
	slices.SortFunc(out.Runs, func(a, b platform.ApplicationRun) int {
		failed := func(run platform.ApplicationRun) bool {
			return run.Error != "" || slices.Contains([]string{"failed", "stuck", "blocked", "stopped", "rejected"}, run.State)
		}
		if failed(a) != failed(b) {
			if failed(a) {
				return -1
			}
			return 1
		}
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

func RunGraph(root platform.AssetRef, assets map[platform.AssetRef]platform.ReleaseAsset) map[platform.AssetRef]bool {
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

func (ctx Context) Graphs(root platform.AssetRef) (map[platform.AssetRef]platform.ReleaseAsset, map[string]map[platform.AssetRef]bool, error) {
	available, err := ctx.Assets()
	if err != nil {
		return nil, nil, err
	}
	current := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, a := range available {
		current[a.Ref] = a
	}
	graphs := map[string]map[platform.AssetRef]bool{"": RunGraph(root, current)}
	for id, raw := range ctx.Candidates() {
		candidate, err := platform.ReadCandidate(id, raw)
		if err != nil {
			return nil, nil, err
		}
		assets := map[platform.AssetRef]platform.ReleaseAsset{}
		for _, a := range candidate.Assets {
			assets[a.Ref] = a
		}
		if _, ok := assets[root]; ok {
			graphs[id] = RunGraph(root, assets)
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
