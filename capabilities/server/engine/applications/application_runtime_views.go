package applications

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/engine/business"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

type Context struct {
	Tenant          string
	Lock, Unlock    func()
	Admit           func(platform.Member) *kernel.Error
	Definitions     func(platform.Member) []platform.Definition
	Entity          func(string) (platform.EntityInfo, bool)
	App             func(string) platform.App
	Assets          func() ([]platform.ReleaseAsset, error)
	Candidate       func(string) (platform.ReleaseCandidate, error)
	Candidates      func() map[string]json.RawMessage
	CloseCandidate  func([]platform.AssetRef, []platform.ReleaseAsset, []platform.AssetRef) (platform.ReleaseCandidate, error)
	Rows            func(platform.Member, string, platform.Query, time.Time) ([]any, *kernel.Error)
	Computations    func(platform.Member) []Computation
	Effects         func(platform.Member) ([]platform.Effect, *kernel.Error)
	Jobs            func(platform.Member) ([]Job, *kernel.Error)
	Readable        func(platform.Member, string, time.Time) bool
	Quarantined     func() bool
	RecoveryAllowed func(platform.Member) *kernel.Error
	Quota           func(platform.Member, time.Time) *platform.AIQuota
	Refusals        func() []Refusal
	BusinessFacts   func() []platform.BusinessRefusalFact
	ActiveVersions  func() []ActiveVersion
	AI              func(platform.Member, []platform.ReleaseAsset, map[platform.AssetRef]bool) ([]platform.ApplicationAI, *kernel.Error)
	AIUsage         func(platform.Member, platform.AssetRef, []platform.ApplicationAI) []platform.ApplicationAI
	RecordCount     func(platform.Member, string, time.Time) (int, *kernel.Error)
}
type Computation struct {
	Effect platform.Effect
	Ref    platform.AssetRef
	Title  string
	Call   platform.OperationCall
	Result platform.OperationResult
}
type Job struct {
	Applications                       []platform.AssetBinding
	ID, Kind, App, Title, State, Error string
	Last                               time.Time
	Withheld                           bool
}
type Refusal struct {
	Submission json.RawMessage
	Error      kernel.Error
	Business   *platform.BusinessRefusalFact
	Issues     []platform.FieldIssue
}
type ActiveVersion struct {
	Candidate platform.ReleaseCandidate
	At        time.Time
}

type functionRun struct {
	platform.Record
	Applications                                                []platform.AssetBinding
	App, Function, Source, State, Release, Dependencies, Reason string
	Contract                                                    platform.AIFunction
	Version, InputTokens, OutputTokens                          int
	CostUSD                                                     float64
	CostReported, Withheld                                      bool
}

func Applications(definitions []platform.Definition) []platform.ApplicationView {
	pages := map[string]*platform.Page{}
	for i := range definitions {
		if definition := definitions[i]; definition.Ref.Kind == platform.AssetPage && definition.Page != nil {
			pages[pageKey(definition.Ref.App, definition.Ref.Name)] = definition.Page
		}
	}
	out := []platform.ApplicationView{}
	for _, definition := range definitions {
		if definition.Ref.Kind != platform.AssetApp || definition.Application == nil {
			continue
		}
		application := *definition.Application
		view := platform.ApplicationView{App: definition.Ref.App, Name: definition.Ref.Name, Title: application.Title,
			Description: application.Description, Icon: application.Icon, Source: definition.Source, Pages: []platform.ApplicationPage{}}
		if view.Title == "" {
			view.Title = view.Name
		}
		for _, name := range application.Pages {
			page := pages[pageKey(definition.Ref.App, name)]
			if page == nil {
				continue
			}
			view.Pages = append(view.Pages, platform.ApplicationPage{Name: page.Name, Title: page.Title,
				Description: page.Description, Object: page.Object.Name})
		}
		if len(view.Pages) == 0 {
			continue
		}
		out = append(out, view)
	}
	slices.SortFunc(out, func(a, b platform.ApplicationView) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func PageView(definitions []platform.Definition, entities []platform.EntityInfo, catalog []platform.Action, app, owner, page string) (platform.PageView, *kernel.Error) {
	var application *platform.Definition
	for i := range definitions {
		definition := &definitions[i]
		if definition.Ref.Kind != platform.AssetApp || definition.Application == nil || definition.Ref.Name != app {
			continue
		}
		if owner != "" && definition.Ref.App != owner {
			continue
		}
		if application == nil || definition.Source == "code" {
			application = definition
		}
	}
	if application == nil {
		return platform.PageView{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This workspace offers no such application")
	}
	wanted := false
	for _, name := range application.Application.Pages {
		wanted = wanted || name == page
	}
	var declared *platform.Page
	if wanted {
		for i := range definitions {
			definition := &definitions[i]
			if definition.Ref.Kind == platform.AssetPage && definition.Page != nil &&
				definition.Ref.App == application.Ref.App && definition.Ref.Name == page {
				declared = definition.Page
				break
			}
		}
	}
	if declared == nil {
		return platform.PageView{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This application has no such page")
	}
	view := platform.PageView{Page: *declared, Actions: []platform.Action{}, Application: platform.ApplicationView{
		App: application.Ref.App, Name: application.Ref.Name, Title: application.Application.Title,
		Description: application.Application.Description, Icon: application.Application.Icon, Source: application.Source}}
	for _, other := range definitions {
		if other.Ref.Kind == platform.AssetPage && other.Ref.App == application.Ref.App && other.Page != nil {
			view.Application.Pages = append(view.Application.Pages, platform.ApplicationPage{Name: other.Page.Name, Title: other.Page.Title,
				Description: other.Page.Description, Object: other.Page.Object.Name})
		}
	}
	slices.SortFunc(view.Application.Pages, func(a, b platform.ApplicationPage) int {
		return indexIn(application.Application.Pages, a.Name) - indexIn(application.Application.Pages, b.Name)
	})
	if declared.Object.Name != "" {
		for _, entity := range entities {
			if entity.Type == declared.Object.Name {
				info := entity
				view.Entity = &info
				break
			}
		}
	}
	for _, trigger := range declared.ActionTriggers {
		if trigger.Binding != "page" {
			continue
		}
		for _, ref := range trigger.Actions {
			for _, action := range catalog {
				if action.Schema == ref.Name {
					view.Actions = append(view.Actions, action)
					break
				}
			}
		}
	}
	return view, nil
}

func pageKey(app, name string) string { return app + "\x00" + name }

func indexIn(names []string, name string) int {
	if i := slices.Index(names, name); i >= 0 {
		return i
	}
	return len(names)
}

func (ctx Context) Bind(resource platform.AssetRef, release, dependencies string, jobOwner ...string) ([]platform.AssetBinding, error) {
	available, err := ctx.Assets()
	if err != nil {
		return nil, err
	}
	if release != "" && dependencies != "" {
		candidate, err := ctx.Candidate(release)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == resource }) {
			available = candidate.Assets
		}
	}
	lookup := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, asset := range available {
		lookup[asset.Ref] = asset
	}
	out := []platform.AssetBinding{}
	for _, root := range available {
		if root.Ref.Kind != platform.AssetApp {
			continue
		}
		graph := RunGraph(root.Ref, lookup)
		uses := graph[resource]
		if len(jobOwner) > 0 {
			for ref := range graph {
				if ref.App == jobOwner[0] {
					uses = true
					break
				}
			}
		}
		if !uses {
			continue
		}
		candidate, err := ctx.CloseCandidate([]platform.AssetRef{root.Ref}, available, []platform.AssetRef{root.Ref})
		if err != nil {
			continue
		}
		if dependencies != "" {
			selected, err := ctx.CloseCandidate([]platform.AssetRef{resource}, candidate.Assets, []platform.AssetRef{resource})
			if err != nil || selected.ID != dependencies {
				continue
			}
		}
		out = append(out, platform.AssetBinding{Ref: root.Ref, SourceVersion: candidate.ID})
	}
	slices.SortFunc(out, func(a, b platform.AssetBinding) int { return CompareRef(a.Ref, b.Ref) })
	return out, nil
}

func CompareRef(a, b platform.AssetRef) int {
	if a.String() < b.String() {
		return -1
	}
	if a.String() > b.String() {
		return 1
	}
	return 0
}

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
	ctx.Lock()
	defer ctx.Unlock()
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

func (ctx Context) maintenance(artifact platform.ApplicationArtifact) platform.ApplicationMaintenance {
	out := platform.ApplicationMaintenance{Extensions: []platform.ApplicationExtension{}, Retention: []platform.ApplicationRetention{}}
	models := map[string]bool{}
	for _, binding := range artifact.Model {
		if binding.Ref.Kind == platform.AssetObject {
			models[binding.Ref.Name] = true
		}
	}
	for _, binding := range artifact.Model {
		if binding.Ref.Kind != platform.AssetObject {
			continue
		}
		info, ok := ctx.Entity(binding.Ref.Name)
		if ok && info.Extends != "" {
			extension := platform.ApplicationExtension{Resource: binding, Base: info.Extends}
			if info.ExtensionSource != nil && models[info.ExtensionSource.Base.Ref.Name] {
				source := info.ExtensionSource.Base
				extension.Source = &source
			}
			out.Extensions = append(out.Extensions, extension)
		}
	}
	if artifact.Profile != nil {
		for category, duration := range artifact.Profile.Retention {
			out.Retention = append(out.Retention, platform.ApplicationRetention{Category: category, Duration: duration, State: "declared"})
		}
		slices.SortFunc(out.Retention, func(a, b platform.ApplicationRetention) int { return strings.Compare(a.Category, b.Category) })
	}
	return out
}

func (ctx Context) Runtime(m platform.Member, ref platform.AssetRef, now time.Time) (platform.ApplicationRuntime, *kernel.Error) {
	out := platform.ApplicationRuntime{Scope: "authorized", State: "healthy", Versions: []platform.ApplicationVersion{}, Data: []platform.ApplicationData{}, Warnings: []platform.ApplicationWarning{}}
	if ref.Kind != platform.AssetApp || ref.Check() != nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a published application")
	}
	ctx.Lock()
	if err := ctx.Admit(m); err != nil {
		ctx.Unlock()
		return out, err
	}
	if ctx.Quarantined() {
		if refusal := ctx.RecoveryAllowed(m); refusal != nil {
			ctx.Unlock()
			return out, refusal
		}
		out.State = "isolated"
		out.Artifact = platform.ApplicationArtifact{Ref: ref, Model: []platform.AssetBinding{}, Rules: []platform.AssetBinding{}, Pages: []platform.AssetBinding{}, Flows: []platform.AssetBinding{}, AI: []platform.AssetBinding{}, Integrations: []platform.AssetBinding{}, Roles: []platform.ApplicationRole{}, Depends: []platform.AssetBinding{}, Environment: []platform.ApplicationBinding{}}
		out.Recovery = platform.ApplicationRecovery{State: "isolated", Predecessors: "unverified", Quarantined: true, Scope: "tenant"}
		ctx.Unlock()
		return out, nil
	}
	visible := map[platform.AssetRef]bool{}
	fields := map[platform.AssetRef]map[string]bool{}
	for _, definition := range ctx.Definitions(m) {
		visible[definition.Ref] = true
		if definition.Application != nil {
			for _, resource := range definition.Application.Resources {
				if resource.Kind == platform.AssetFlow {
					visible[resource] = true
				}
			}
		}
		if definition.Entity != nil {
			fields[definition.Ref] = map[string]bool{}
			for _, field := range definition.Entity.Fields {
				fields[definition.Ref][field.Name] = true
			}
		}
	}
	if !visible[ref] {
		ctx.Unlock()
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Application is unavailable to this member")
	}
	assets, err := ctx.Assets()
	if err != nil {
		ctx.Unlock()
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
	}
	candidate, err := ctx.CloseCandidate([]platform.AssetRef{ref}, assets, []platform.AssetRef{ref})
	if err != nil {
		ctx.Unlock()
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
	}
	artifact, err := platform.ArtifactOfApplication(ref, candidate.Assets)
	if err != nil {
		ctx.Unlock()
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
	}
	out.Artifact = artifact
	out.Monitoring.AgentBudgets = []platform.ApplicationAgentBudget{}
	if len(artifact.AI) > 0 {
		out.Monitoring.MemberQuota = ctx.Quota(m, now)
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind != platform.AssetAgent || !visible[asset.Ref] {
			continue
		}
		var descriptor platform.AgentReleaseDescriptor
		if err := json.Unmarshal(asset.Body, &descriptor); err != nil {
			ctx.Unlock()
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application runtime dependencies are unavailable")
		}
		out.Monitoring.AgentBudgets = append(out.Monitoring.AgentBudgets, platform.ApplicationAgentBudget{Resource: platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}, Budget: descriptor.Budget})
	}
	model := map[string]bool{}
	for _, binding := range artifact.Model {
		if visible[binding.Ref] {
			model[binding.Ref.Name] = true
		}
	}
	accepted := map[string]bool{}
	owners := map[string]bool{}
	for _, binding := range artifact.Model {
		owner := ctx.App(binding.Ref.App)
		if owner == nil || !visible[binding.Ref] || binding.Ref.Kind != platform.AssetObject || owners[binding.Ref.App] {
			continue
		}
		owners[binding.Ref.App] = true
		for _, receipt := range owner.AcceptedLedger().RecordsFor(ctx.Tenant) {
			sub := receipt.GetSubmission()
			if sub == nil || !model[sub.GetTarget().GetType()] || sub.GetPrincipalId() != m.ID && !ctx.Readable(m, sub.GetTarget().GetType()+"/"+sub.GetTarget().GetId(), now) {
				continue
			}
			accepted[binding.Ref.App+"/"+receipt.GetChangeId()] = true
		}
	}
	out.Monitoring.Accepted = len(accepted)
	codeCounts := map[string]platform.RefusalCodeCount{}
	for _, refusal := range ctx.Refusals() {
		var sub pb.Submission
		if protojson.Unmarshal(refusal.Submission, &sub) != nil || !model[sub.GetTarget().GetType()] || sub.GetPrincipalId() != m.ID && !ctx.Readable(m, sub.GetTarget().GetType()+"/"+sub.GetTarget().GetId(), now) {
			continue
		}
		out.Monitoring.Refused++
		category, code := business.RefusalCategory(refusal.Error, refusal.Business, refusal.Issues)
		if category == "business" {
			out.Monitoring.BusinessRefusals++
		}
		codeKey := category + "/" + sub.GetTarget().GetType() + "/" + code
		count := codeCounts[codeKey]
		count.Category, count.Code, count.Type = category, code, sub.GetTarget().GetType()
		count.Count++
		codeCounts[codeKey] = count
		if slices.ContainsFunc(refusal.Issues, func(issue platform.FieldIssue) bool { return issue.Constraint != "" }) {
			out.Monitoring.ConstraintFailures++
		}
	}
	for _, fact := range ctx.BusinessFacts() {
		typ, _, _ := strings.Cut(fact.Subject, "/")
		if !model[typ] || fact.Member != m.ID && !ctx.Readable(m, fact.Subject, now) {
			continue
		}
		out.Monitoring.BusinessRefusals++
		key := "business/" + typ + "/" + fact.Code
		count := codeCounts[key]
		count.Category, count.Code, count.Type = "business", fact.Code, typ
		count.Count++
		codeCounts[key] = count
	}
	for _, count := range codeCounts {
		out.Monitoring.RefusalCodes = append(out.Monitoring.RefusalCodes, count)
	}
	slices.SortFunc(out.Monitoring.RefusalCodes, func(a, b platform.RefusalCodeCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Category+"/"+a.Type+"/"+a.Code, b.Category+"/"+b.Type+"/"+b.Code)
	})
	if count := out.Monitoring.Accepted + out.Monitoring.Refused; count > 0 {
		out.Monitoring.RefusalRate = float64(out.Monitoring.Refused) / float64(count)
	}
	for _, release := range ctx.ActiveVersions() {
		candidate := release.Candidate
		if !slices.ContainsFunc(candidate.Assets, func(asset platform.ReleaseAsset) bool { return asset.Ref == ref }) {
			continue
		}
		version, err := platform.ArtifactOfApplication(ref, candidate.Assets)
		if err != nil {
			ctx.Unlock()
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Application version history is unavailable")
		}
		out.Versions = append(out.Versions, platform.ApplicationVersion{Version: version.Version, Release: candidate.ID, At: release.At, Active: version.Version == artifact.Version})
	}
	slices.SortFunc(out.Versions, func(a, b platform.ApplicationVersion) int {
		if a.At.After(b.At) {
			return -1
		}
		if a.At.Before(b.At) {
			return 1
		}
		return strings.Compare(a.Release, b.Release)
	})
	for _, version := range out.Versions {
		if !version.Active {
			out.Previous = version.Version
			break
		}
	}
	if len(out.Versions) == 0 {
		out.Versions = append(out.Versions, platform.ApplicationVersion{Version: artifact.Version, Active: true})
	}
	project := func(bindings []platform.AssetBinding) []platform.AssetBinding {
		return slices.DeleteFunc(slices.Clone(bindings), func(binding platform.AssetBinding) bool { return !visible[binding.Ref] })
	}
	out.Artifact.Model, out.Artifact.Rules, out.Artifact.Pages = project(artifact.Model), project(artifact.Rules), project(artifact.Pages)
	out.Artifact.Flows, out.Artifact.AI, out.Artifact.Integrations = project(artifact.Flows), project(artifact.AI), project(artifact.Integrations)
	out.Artifact.Depends = project(artifact.Depends)
	out.Artifact.Environment = slices.DeleteFunc(slices.Clone(artifact.Environment), func(binding platform.ApplicationBinding) bool { return !visible[binding.Ref] })
	out.Artifact.Roles = slices.DeleteFunc(slices.Clone(artifact.Roles), func(role platform.ApplicationRole) bool {
		return !visible[role.Resource.Ref] || !slices.Contains(m.RolesIn(role.Resource.Ref.App), "builder") && !slices.Contains(m.RolesIn(role.Resource.Ref.App), role.Role)
	})
	for i := range out.Artifact.Roles {
		role := &out.Artifact.Roles[i]
		role.ReadFields = slices.DeleteFunc(slices.Clone(role.ReadFields), func(name string) bool { return !fields[role.Resource.Ref][name] })
		role.WriteFields = slices.DeleteFunc(slices.Clone(role.WriteFields), func(name string) bool { return !fields[role.Resource.Ref][name] })
	}
	if artifact.Profile != nil {
		profile := *artifact.Profile
		profile.ExpectedRecords = map[string]int64{}
		for _, binding := range out.Artifact.Model {
			if count := artifact.Profile.ExpectedRecords[binding.Ref.Name]; count > 0 {
				profile.ExpectedRecords[binding.Ref.Name] = count
			}
		}
		out.Artifact.Profile = &profile
	}
	out.Maintenance = ctx.maintenance(out.Artifact)
	var refusal *kernel.Error
	out.AI, refusal = ctx.AI(m, candidate.Assets, visible)
	if refusal != nil {
		ctx.Unlock()
		return out, refusal
	}
	ctx.Unlock()
	out.AI = ctx.AIUsage(m, ref, out.AI)
	for _, binding := range out.Artifact.Model {
		if binding.Ref.Kind != platform.AssetObject {
			continue
		}
		count, refusal := ctx.RecordCount(m, binding.Ref.Name, now)
		if refusal != nil {
			out.State = "degraded"
			continue
		}
		data := platform.ApplicationData{Ref: binding.Ref, Records: count}
		if artifact.Profile != nil {
			data.Expected = artifact.Profile.ExpectedRecords[binding.Ref.Name]
		}
		out.Data = append(out.Data, data)
		if data.Expected > 0 && int64(data.Records) > data.Expected {
			out.Warnings = append(out.Warnings, platform.ApplicationWarning{Kind: "records", Resource: binding.Ref.Name, Actual: int64(data.Records), Expected: data.Expected})
		}
	}
	runs, refusal := ctx.Runs(m, ref, 0, 100, now)
	if refusal != nil {
		return out, refusal
	}
	out.Runs = runs.Total
	if artifact.Profile != nil && artifact.Profile.ExpectedRuns > 0 && int64(out.Runs) > artifact.Profile.ExpectedRuns {
		out.Warnings = append(out.Warnings, platform.ApplicationWarning{Kind: "runs", Actual: int64(out.Runs), Expected: artifact.Profile.ExpectedRuns})
	}
	out.Recovery.FailedRuns, out.Recovery.PendingEffects = runs.Summary.Failed, runs.Summary.PendingEffects
	out.Monitoring.Runs = runs.Summary
	if len(out.Warnings) > 0 || out.Recovery.FailedRuns > 0 {
		out.State = "degraded"
	}
	out.Recovery.State = "ready"
	out.Recovery.Predecessors, out.Recovery.Scope = "admitted", "authorized"
	if out.Recovery.PendingEffects > 0 || out.Recovery.FailedRuns > 0 {
		out.Recovery.State = "attention"
	}
	return out, nil
}
