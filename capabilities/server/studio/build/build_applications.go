package build

import (
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"slices"
	"time"
)

// An application someone hands to the people it was built for: a
// name, an icon and the pages it holds. It grants nothing — each page is
// offered to whoever may read what it shows — and the workspace turns it into
// an entry in the launcher beside the apps that came as code.

// Application is an application this organisation hands to its people.
type Application struct {
	platform.Record
	Languages    platform.Languages                    `json:"languages,omitempty" field:"aside" type:"json" title:"Application translations"`
	Profile      *platform.ApplicationProfile          `json:"profile,omitempty" field:"aside" type:"json" title:"Application profile" help:"Expected records and runs, retention and recovery declarations; actual guarantees require environment evidence"`
	Header       *platform.ApplicationHeader           `json:"header,omitempty" field:"aside" type:"json" title:"Application header"`
	UIProfile    string                                `json:"uiProfile,omitempty" field:"aside" title:"UI profile"`
	Queries      map[string]platform.PageQuery         `json:"queries,omitempty" field:"aside" type:"json" title:"Application queries"`
	Extends      *platform.AssetRef                    `json:"extends,omitempty" field:"aside" type:"json"`
	Entry        string                                `json:"entry,omitempty" field:"aside"`
	Replacements []platform.ApplicationPageReplacement `json:"replacements,omitempty" field:"aside" type:"json"`
	Variables    map[string]platform.PageVariable      `json:"variables,omitempty" field:"aside" type:"json" title:"Application variables"`
	Name         string                                `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"frontdesk"`
	Title        string                                `json:"title" field:"required,search" title:"What people call it" example:"Front desk"`
	Description  string                                `json:"description,omitempty" type:"longtext" help:"What people do in it"`
	Icon         string                                `json:"icon,omitempty" help:"How it is drawn in the launcher"`
	Pages        []string                              `json:"pages" title:"Pages" help:"The pages it holds, by their name, in the order people see them"`
	// Groups are the headings of its navigation (17b); a page in none of them
	// sits under the application's own name.
	Resources []platform.AssetRef   `json:"resources,omitempty" field:"aside" title:"Application resources" help:"Published objects, workflows and functions included in its release; ownership and permissions stay with each resource"`
	Concepts  []platform.AppConcept `json:"concepts,omitempty" field:"aside" type:"json" title:"Business concepts" help:"Named studio groups; grouping changes navigation only"`
	Groups    []Group               `json:"groups,omitempty" title:"Groups" help:"Headings in its navigation, each over some of its pages in their order"`
	State     string                `json:"state" field:"readonly" choices:"draft,published,withdrawn"`
	// Published is the application as it was last handed over: what people open,
	// and what a restore puts back.
	Published string `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
}

func (b *Build) applicationEntity() platform.Entity {
	return platform.Entity{Type: AppType, Title: "Application", Plural: "Applications", Model: Application{}, Display: "title",
		Description: "An application this organisation hands to its people: a name, an icon, its pages and shared resource references. It gives nobody new access; each page is offered to whoever may read what it shows.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			a := record.(*Application)
			if err := applicationDescriptor(*a).CheckLanguages(); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			if err := applicationDescriptor(*a).CheckExtension(); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			if err := applicationDescriptor(*a).CheckConcepts(); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			d := platform.PageDocument{UIProfile: platform.PageUIProfile(), Variables: a.Variables}
			if err := d.NormalizePageRules(c.Rules()); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			a.Variables = d.Variables
			if a.Profile != nil {
				if err := a.Profile.Check(); err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
			}
			return nil
		},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "applications"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning", Description: "Being put together; nobody has it yet."},
				{Name: "published", Title: "Published", Tone: "success", Description: "In the launcher of everyone who may open one of its pages."},
				{Name: "withdrawn", Title: "Entry withdrawn", Tone: "warning", Description: "The application entry is closed; retained definitions, records and independent tasks remain."}},
			Transitions: []platform.Transition{
				{Name: "withdraw", Title: "Withdraw application entry", From: []string{"published"}, To: []string{"withdrawn"}, Roles: []string{Builder}, Capability: "applications", Payload: []platform.Field{}},
				{Name: "restore", Title: "Restore application entry", From: []string{"withdrawn"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "applications", Payload: []platform.Field{}},
				{Name: "publish", Title: "Hand it over", From: []string{"draft", "published", "withdrawn"}, To: []string{"published"},
					Roles: []string{Builder}, Capability: "applications", Payload: []platform.Field{},
					Description: "Put the application in people's launcher as it stands. Handed over again, it takes its new pages and name.",
					Do: func(c platform.Caller, record any, _ json.RawMessage, now time.Time) *kernel.Error {
						application, ok := record.(*Application)
						if !ok {
							return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
						}
						if c.Staging() {
							if err := b.checkApplication(*application); err != nil {
								return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
							}
							if err := b.host.ValidateInstallApplication(applicationDescriptor(*application)); err != nil {
								return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
							}
						} else if err := b.hand(c, *application); err != nil {
							return err
						}
						application.Published = published(*application)
						return nil
					}}}}}
}

// hand installs the application: the host checks that every page it names is
// there, and offers it to whoever may open one of them.
func (b *Build) hand(c platform.Caller, a Application) *kernel.Error {
	if err := b.checkApplication(a); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if err := b.host.InstallApplication(c, applicationDescriptor(a)); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	return nil
}

// application is the descriptor the registry holds.
func applicationDescriptor(a Application) platform.Application {
	groups := make([]platform.AppGroup, 0, len(a.Groups))
	for _, g := range a.Groups {
		groups = append(groups, platform.AppGroup{Title: g.Title, Pages: slices.Clone(g.Pages)})
	}
	return platform.Application{Languages: a.Languages, Profile: a.Profile, Header: a.Header, Name: a.Name, Title: a.Title, Description: a.Description, Icon: a.Icon, Pages: slices.Clone(a.Pages), Groups: groups, Resources: slices.Clone(a.Resources), Concepts: a.Concepts, UIProfile: a.UIProfile, Variables: a.Variables, Queries: a.Queries, Extends: a.Extends, Entry: a.Entry, Replacements: slices.Clone(a.Replacements)}
}

// checkApplication refuses an application people could not open: a name that is
// not a name, no pages, an icon name that is not one.
func (b *Build) checkApplication(a Application) error {
	if err := b.checkName(a.Name, a.ID); err != nil {
		return err
	}
	if len(a.Pages) == 0 && len(a.Resources) == 0 {
		return fmt.Errorf("an application needs pages or declared resources")
	}
	if a.Icon != "" && !platform.IconName(a.Icon) {
		return fmt.Errorf("%q is not an icon name: lower-case letters, digits and dashes", a.Icon)
	}
	seen := map[string]bool{}
	for _, name := range a.Pages {
		if seen[name] {
			return fmt.Errorf("the page %q is in it twice", name)
		}
		seen[name] = true
	}
	if err := applicationDescriptor(a).CheckResources(); err != nil {
		return err
	}
	if err := applicationDescriptor(a).CheckVariables(); err != nil {
		return err
	}
	if err := applicationDescriptor(a).CheckHeader(); err != nil {
		return err
	}
	return applicationDescriptor(a).CheckGroups()
}

const (
	AppType        = "build.app"
	SchemaHandOver = AppType + ".publish"
	ValuePagesHelp = "The pages it holds, by their name, in the order people see them"
)

// Group is one heading in an application's navigation.
type Group struct {
	Title string   `json:"title" field:"required" title:"Heading"`
	Pages []string `json:"pages" title:"Pages" help:"Pages of the application, by their name, in their order"`
}

// ApplicationProjection receives only the declarations already offered by
// their permission owners. PublishedFlows is a frozen read of the original
// Flow owner; this projection cannot discover or execute a flow by itself.
type ApplicationProjection struct {
	Member         platform.Member
	Definitions    []platform.Definition
	Entities       map[string]platform.EntityInfo
	Pages          map[platform.AssetRef]bool
	Resources      map[platform.AssetRef]bool
	PublishedFlows map[platform.AssetRef]bool
}

// ProjectApplication removes pages, resource bindings and dependent variables
// the member cannot use, without changing the registered application image.
func ProjectApplication(registered platform.Definition, projection ApplicationProjection) (platform.Definition, bool) {
	m, visible, entities := projection.Member, projection.Definitions, projection.Entities
	opens, visibleResources := projection.Pages, projection.Resources
	if registered.Ref.Kind != platform.AssetApp || registered.Application == nil || m.Roles[registered.Ref.App] == "" {
		return platform.Definition{}, false
	}
	def, application := registered, *registered.Application
	closed := func(name string) bool {
		return !opens[platform.AssetRef{App: def.Ref.App, Kind: platform.AssetPage, Name: name}]
	}
	application.Pages = slices.DeleteFunc(slices.Clone(application.Pages), closed)
	if len(application.Pages) == 0 && len(application.Resources) == 0 && registered.Source != "code" {
		return platform.Definition{}, false
	}
	// A heading over none of this member's pages is not in their navigation.
	groups := make([]platform.AppGroup, 0, len(application.Groups))
	for _, g := range application.Groups {
		if g.Pages = slices.DeleteFunc(slices.Clone(g.Pages), closed); len(g.Pages) > 0 {
			groups = append(groups, g)
		}
	}
	application.Groups = groups
	application.Header = application.VisibleHeader()
	application.Resources = slices.DeleteFunc(slices.Clone(application.Resources), func(ref platform.AssetRef) bool {
		if ref.Kind == platform.AssetFlow {
			return !m.Holds("build", "builder") || !projection.PublishedFlows[ref]
		}
		return !visibleResources[ref]
	})
	if len(application.Pages) == 0 && len(application.Resources) == 0 && registered.Source != "code" {
		return platform.Definition{}, false
	}
	application.Replacements = slices.DeleteFunc(slices.Clone(application.Replacements), func(replacement platform.ApplicationPageReplacement) bool {
		return !slices.Contains(application.Pages, replacement.Page)
	})
	application.Queries = maps.Clone(application.Queries)
	application.Variables = maps.Clone(application.Variables)
	for id, q := range application.Queries {
		info, ok := entities[q.Object.Name]
		var named *platform.Definition
		if q.Query != nil {
			for _, d := range visible {
				if d.Ref == q.Query.Ref {
					named = d.QuerySourceVersion(q.Query.SourceVersion)
					break
				}
			}
		}
		if named != nil && named.LinkType != nil {
			l := named.LinkType
			parent, pok := entities[l.Parent.Name]
			child, cok := entities[l.Child.Name]
			if !pok || !cok || l.CheckSchema(parent, child) != nil {
				named = nil
			}
		}
		if !ok || application.QueryPage().CheckQuerySchema(q, info, named) != nil {
			delete(application.Queries, id)
		}
	}
	for changed := true; changed; {
		changed = false
		for id, q := range application.Queries {
			missing := q.MissingSetInput(application.Queries)
			for _, param := range q.Variables() {
				if _, ok := application.Variables[param]; !ok {
					missing = true
				}
			}
			if missing {
				delete(application.Queries, id)
				changed = true
			}
		}
		for id, v := range application.Variables {
			missing := false
			if v.Mode == "property" && v.Source != nil && v.Source.Object != nil {
				info, ok := entities[v.Source.Object.Name]
				_, parent := application.Variables[v.Source.Variable]
				missing = !ok || !parent || application.QueryPage().CheckPropertySchema(v, info) != nil
			}
			if v.Mode == "aggregate" && v.Source != nil {
				_, ok := application.Queries[v.Source.Query]
				missing = !ok
			}
			if v.Mode == "resource" && v.Source != nil {
				if (v.Source.Kind == "record" || v.Source.Kind == "filter" || v.Source.Kind == "record-set") && v.Source.Object != nil {
					_, ok := entities[v.Source.Object.Name]
					missing = !ok
					if ok && v.Type == "filter" {
						source := *v.Source
						info := entities[source.Object.Name]
						source.Fields = slices.DeleteFunc(slices.Clone(source.Fields), func(name string) bool {
							field, exists := info.Field(name)
							return !exists || (field.Type != "choice" && field.Type != "boolean" && field.Type != "reference")
						})
						v.Source = &source
						application.Variables[id] = v
						missing = len(source.Fields) == 0
					}
				} else {
					_, ok := application.Queries[v.Source.Query]
					missing = !ok
				}
			}
			if v.Expression != nil {
				for _, arg := range v.Expression.Args {
					if arg.Variable != "" {
						if _, ok := application.Variables[arg.Variable]; !ok {
							missing = true
						}
					}
				}
			}
			if missing {
				delete(application.Variables, id)
				changed = true
			}
		}
	}
	def.Application = &application
	return def, true
}
