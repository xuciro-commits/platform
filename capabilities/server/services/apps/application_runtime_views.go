package applications

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

type Context struct {
	Tenant          string
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
