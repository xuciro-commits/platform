package apps

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/resources/publication"
	"slices"
	"strings"
	"time"
)

type Context struct {
	Tenant              string
	Resolver            publication.Resolver
	Admit               func(platform.Member) *platform.Error
	Definitions         func(platform.Member) []platform.Definition
	Entity              func(string) (platform.RecordTypeInfo, bool)
	App                 func(string) platform.Service
	Assets              func() ([]platform.ReleaseAsset, error)
	Candidate           func(string) (protocol.ReleaseCandidate, error)
	Candidates          func() map[string]json.RawMessage
	CloseCandidate      func([]platform.AssetRef, []platform.ReleaseAsset, []platform.AssetRef) (protocol.ReleaseCandidate, error)
	Rows                func(platform.Member, string, platform.Query, time.Time) ([]any, *platform.Error)
	Computations        func(platform.Member) []Computation
	Effects             func(platform.Member) ([]platform.Effect, *platform.Error)
	Jobs                func(platform.Member) ([]Job, *platform.Error)
	Readable            func(platform.Member, string, time.Time) bool
	Quarantined         func() bool
	RecoveryAllowed     func(platform.Member) *platform.Error
	Quota               func(platform.Member, time.Time) *platform.AIQuota
	Refusals            func(field ...string) map[string]acceptedformat.Refusal
	Answers             func(field ...string) map[string]json.RawMessage
	AI                  func(platform.Member, []platform.ReleaseAsset, map[platform.AssetRef]bool) ([]protocol.ApplicationAI, *platform.Error)
	AIUsage             func(platform.Member, platform.AssetRef, []protocol.ApplicationAI) []protocol.ApplicationAI
	RecordCount         func(platform.Member, string, time.Time) (int, *platform.Error)
	AuthorizationMember func(platform.Member) platform.Member
	RefreshRunIndex     func(key string, now time.Time, rebuild func() bool)
	ReplaceRunIndex     func(key string, entries []protocol.RunIndexEntry) error
	QueryRunIndex       func(scope string) ([]protocol.RunIndexEntry, error)
	RuntimeOf           func(platform.Member, platform.AssetRef, time.Time) (protocol.ApplicationRuntime, *platform.Error)
	RunsPage            func(platform.Member, platform.AssetRef, int, int, time.Time, ...protocol.ApplicationRunFilter) (protocol.ApplicationRunPage, *platform.Error)
	RecordHistory       func(platform.Member, string, string, time.Time) ([]records.RecordChange, *platform.Error)
	RecordOwner         func(typ string) string
	Receipts            func(owner string) []*pb.ChangeRecord
	Decision            func(key string) *platform.DecisionDefinition
	SchemaVersion       func(key string) (platform.SchemaVersion, bool)
	VisibleEvidence     func(platform.Member, *platform.ObservationDecisionEvidence, time.Time) *platform.ObservationDecisionEvidence
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

type ActiveVersion struct {
	Candidate protocol.ReleaseCandidate
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

func Applications(definitions []platform.Definition) []protocol.ApplicationView {
	pages := visiblePages(definitions)
	out := []protocol.ApplicationView{}
	for _, definition := range definitions {
		if definition.Ref.Kind != platform.AssetApp || definition.Application == nil {
			continue
		}
		if view := applicationView(definition, pages); len(view.Menu) != 0 {
			out = append(out, view)
		}
	}
	slices.SortFunc(out, func(a, b protocol.ApplicationView) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func visiblePages(definitions []platform.Definition) map[platform.AssetRef]*platform.Page {
	pages := map[platform.AssetRef]*platform.Page{}
	for _, definition := range definitions {
		if definition.Ref.Kind == platform.AssetPage && definition.Page != nil {
			pages[definition.Ref] = definition.Page
		}
	}
	return pages
}

func applicationView(definition platform.Definition, visible map[platform.AssetRef]*platform.Page) protocol.ApplicationView {
	application := definition.Application
	view := protocol.ApplicationView{App: definition.Ref.App, Name: definition.Ref.Name, Title: application.Title,
		Description: application.Description, Icon: application.Icon, Surface: application.Surface, Source: definition.Source}
	if view.Title == "" {
		view.Title = view.Name
	}
	var pages []protocol.ApplicationPage
	for _, name := range application.Pages {
		if page := visible[platform.AssetRef{App: definition.Ref.App, Kind: platform.AssetPage, Name: name}]; page != nil {
			pages = append(pages, protocol.ApplicationPage{Name: page.Name, Title: page.Title,
				Description: page.Description, Object: page.Object.Name})
		}
	}
	view.Menu = applicationMenu(application.Groups, pages)
	json.Unmarshal(application.Dashboard, &view.Dashboard)
	return view
}

func applicationMenu(groups []platform.AppGroup, pages []protocol.ApplicationPage) []protocol.ApplicationMenuGroup {
	visible := make(map[string]protocol.ApplicationPage, len(pages))
	for _, page := range pages {
		visible[page.Name] = page
	}
	var menu []protocol.ApplicationMenuGroup
	used := make(map[string]bool, len(pages))
	for _, group := range groups {
		projected := protocol.ApplicationMenuGroup{Title: group.Title, Layout: group.Layout}
		for _, name := range group.Pages {
			if page, ok := visible[name]; ok {
				projected.Entries = append(projected.Entries, page)
				used[name] = true
			}
		}
		if len(projected.Entries) != 0 {
			menu = append(menu, projected)
		}
	}
	unassigned := protocol.ApplicationMenuGroup{}
	for _, page := range pages {
		if !used[page.Name] {
			unassigned.Entries = append(unassigned.Entries, page)
		}
	}
	if len(unassigned.Entries) != 0 {
		menu = append(menu, unassigned)
	}
	return menu
}

func PageView(definitions []platform.Definition, entities []platform.RecordTypeInfo, catalog []platform.Action, app, owner, page string) (protocol.PageView, *platform.Error) {
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
		return protocol.PageView{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This workspace offers no such application")
	}
	pages := visiblePages(definitions)
	declared := pages[platform.AssetRef{App: application.Ref.App, Kind: platform.AssetPage, Name: page}]
	if declared == nil || !slices.Contains(application.Application.Pages, page) {
		return protocol.PageView{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This application has no such page")
	}
	view := protocol.PageView{Page: *declared, Actions: []platform.Action{}, Application: applicationView(*application, pages)}
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
		graph := ctx.Resolver.RunGraph(root.Ref, lookup)
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
