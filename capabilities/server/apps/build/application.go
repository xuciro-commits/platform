package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// An application someone hands to the people it was built for (ADR-0036): a
// name, an icon and the pages it holds. It grants nothing — each page is
// offered to whoever may read what it shows — and the workspace turns it into
// an entry in the launcher beside the apps that came as code.

const (
	AppType        = "build.app"
	SchemaHandOver = AppType + ".publish"
	iconChoices    = "boxes,clipboard,people,calendar,wrench,map,chart,sparkles"
	pagesHelp      = "The pages it holds, by their name, in the order people see them"
)

// Application is an application this organisation hands to its people.
type Application struct {
	platform.Record
	Name        string   `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"frontdesk"`
	Title       string   `json:"title" field:"required,search" title:"What people call it" example:"Front desk"`
	Description string   `json:"description,omitempty" type:"longtext" help:"What people do in it"`
	Icon        string   `json:"icon,omitempty" choices:"boxes,clipboard,people,calendar,wrench,map,chart,sparkles" help:"How it is drawn in the launcher"`
	Pages       []string `json:"pages" title:"Pages" help:"The pages it holds, by their name, in the order people see them"`
	// Groups are the headings of its navigation (17b); a page in none of them
	// sits under the application's own name.
	Groups []Group `json:"groups,omitempty" title:"Groups" help:"Headings in its navigation, each over some of its pages in their order"`
	State  string  `json:"state" field:"readonly" choices:"draft,published"`
	// Published is the application as it was last handed over: what people open,
	// and what a restore puts back.
	Published string `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
}

// Group is one heading in an application's navigation.
type Group struct {
	Title string   `json:"title" field:"required" title:"Heading"`
	Pages []string `json:"pages" title:"Pages" help:"Pages of the application, by their name, in their order"`
}

func (b *Build) applicationEntity() platform.Entity {
	return platform.Entity{Type: AppType, Title: "Application", Plural: "Applications", Model: Application{}, Display: "title",
		Description: "An application this organisation hands to its people: a name, an icon and the pages it holds. It gives nobody new access; each page is offered to whoever may read what it shows.",
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "applications"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning", Description: "Being put together; nobody has it yet."},
				{Name: "published", Title: "Published", Tone: "success", Description: "In the launcher of everyone who may open one of its pages."}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Hand it over", From: []string{"draft", "published"}, To: []string{"published"},
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
	return platform.Application{Name: a.Name, Title: a.Title, Description: a.Description, Icon: a.Icon, Pages: slices.Clone(a.Pages), Groups: groups}
}

// checkApplication refuses an application people could not open: a name that is
// not a name, no pages, an icon the platform does not draw.
func (b *Build) checkApplication(a Application) error {
	if err := b.checkName(a.Name, a.ID); err != nil {
		return err
	}
	if len(a.Pages) == 0 {
		return fmt.Errorf("an application holds at least one page")
	}
	if a.Icon != "" && !slices.Contains(platform.Icons, a.Icon) {
		return fmt.Errorf("there is no icon %q; there are %s", a.Icon, strings.Join(platform.Icons, ", "))
	}
	seen := map[string]bool{}
	for _, name := range a.Pages {
		if seen[name] {
			return fmt.Errorf("the page %q is in it twice", name)
		}
		seen[name] = true
	}
	return applicationDescriptor(a).CheckGroups()
}
