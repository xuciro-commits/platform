package authoring

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	pageowner "platformserver/services/apps/pages"
	ontology "platformserver/services/ontology"
)

func (o *Owner) ApplicationEntity() platform.RecordType {
	return platform.RecordType{Type: protocol.AppType, Title: "Project", Plural: "Projects", Model: protocol.Application{}, Display: "title",
		Description: "An application this organisation hands to its people: a name, an icon, its pages and shared resource references. It gives nobody new access; each page is offered to whoever may read what it shows.",
		Validate: func(c platform.Caller, record any) *platform.Error {
			a := record.(*protocol.Application)
			return o.ValidateApplication(c, a)
		},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{ontology.BuilderRole}, Capability: "applications"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning", Description: "Being put together; nobody has it yet."},
				{Name: "published", Title: "Published", Tone: "success", Description: "In the launcher of everyone who may open one of its pages."},
				{Name: "withdrawn", Title: "Entry withdrawn", Tone: "warning", Description: "The application entry is closed; retained definitions, records and independent tasks remain."}},
			Actions: []platform.ActionType{
				{
					Name:       "withdraw",
					Title:      "Withdraw application entry",
					Roles:      []string{ontology.BuilderRole},
					Capability: "applications",
					Parameters: []platform.Field{},
					Criteria:   platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"published"}}},
					Rules:      []platform.ActionRule{{Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"withdrawn"}}}},
				},
				{
					Name:       "restore",
					Title:      "Restore application entry",
					Roles:      []string{ontology.BuilderRole},
					Capability: "applications",
					Parameters: []platform.Field{},
					Criteria:   platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"withdrawn"}}},
					Rules:      []platform.ActionRule{{Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"published"}}}},
				},
				{
					Name:        "publish",
					Title:       "Hand it over",
					Roles:       []string{ontology.BuilderRole},
					Capability:  "applications",
					Parameters:  []platform.Field{},
					Description: "Put the application in people's launcher as it stands. Handed over again, it takes its new pages and name.",
					Criteria:    platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"draft", "published", "withdrawn"}}},
					Rules:       []platform.ActionRule{{Function: o.publishApplication}, {Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"published"}}}},
				}}}}
}

func (o *Owner) ValidateApplication(c platform.Caller, a *protocol.Application) *platform.Error {
	if c.Replaying {
		return nil
	}
	if err := pageowner.CheckApplicationProfile(a.Profile); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if err := ApplicationDescriptor(*a).CheckLanguages(); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if err := pageowner.CheckApplicationExtension(ApplicationDescriptor(*a)); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if err := pageowner.CheckApplicationConcepts(ApplicationDescriptor(*a)); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if err := pageowner.CheckVariables(platform.PageInputSchema{Variables: a.Variables}, c.Rules()); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	lookup := o.env.Entity
	for _, query := range a.Queries {
		if query.Filter != nil {
			draftLookup, err := o.env.DraftObjectLookup(c)
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			lookup = draftLookup
			break
		}
	}
	if err := pageowner.CheckQueryFilters(pageowner.ApplicationPage(ApplicationDescriptor(*a)), lookup); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return nil
}

func (o *Owner) publishApplication(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *platform.Error {
	var application *protocol.Application
	switch value := record.(type) {
	case *protocol.Application:
		application = value
	default:
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if c.Staging() {
		if err := o.checkApplication(*application); err != nil {
			return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
		if err := o.env.Host().ValidateInstallApplication(ApplicationDescriptor(*application)); err != nil {
			return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
	} else if err := o.installApplication(c, *application); err != nil {
		return err
	}
	application.Published = o.PublishedApplication(*application)
	return nil
}

func (o *Owner) installApplication(c platform.Caller, a protocol.Application) *platform.Error {
	if err := o.checkApplication(a); err != nil {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if err := o.env.Host().InstallApplication(c, ApplicationDescriptor(a)); err != nil {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	return nil
}

func ApplicationDescriptor(a protocol.Application) platform.Application {
	groups := make([]platform.AppGroup, 0, len(a.Groups))
	for _, g := range a.Groups {
		groups = append(groups, platform.AppGroup{Title: g.Title, Roles: slices.Clone(g.Roles), Pages: slices.Clone(g.Pages)})
	}
	return platform.Application{Languages: a.Languages, Profile: a.Profile, Header: a.Header, Name: a.Name, Title: a.Title, Description: a.Description, Icon: a.Icon, Pages: slices.Clone(a.Pages), Groups: groups, Dashboard: dashboardBody(a.Dashboard), Resources: slices.Clone(a.Resources), Concepts: a.Concepts, UIProfile: a.UIProfile, Variables: a.Variables, Queries: a.Queries, Extends: a.Extends, Entry: a.Entry, Replacements: slices.Clone(a.Replacements)}
}

func dashboardBody(blocks []protocol.DashboardBlock) json.RawMessage {
	if len(blocks) == 0 {
		return nil
	}
	body, _ := json.Marshal(blocks)
	return body
}

func (o *Owner) PublishedApplication(a protocol.Application) string {
	a.Published = ""
	return string(platform.Raw(a))
}

func (o *Owner) checkApplication(a protocol.Application) error {
	if err := o.env.CheckName(a.Name, a.ID); err != nil {
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
	if err := pageowner.CheckApplicationResources(ApplicationDescriptor(a)); err != nil {
		return err
	}
	if err := pageowner.CheckApplicationVariables(ApplicationDescriptor(a)); err != nil {
		return err
	}
	if err := pageowner.CheckApplicationHeader(ApplicationDescriptor(a)); err != nil {
		return err
	}
	if err := pageowner.CheckApplicationDashboard(ApplicationDescriptor(a)); err != nil {
		return err
	}
	for _, block := range a.Dashboard {
		if o.env.Host() != nil {
			if _, ok := o.env.Entity(block.Object); !ok {
				return fmt.Errorf("the dashboard block %q reads %q, which this tenant does not have", block.Title, block.Object)
			}
		}
	}
	return pageowner.CheckApplicationGroups(ApplicationDescriptor(a))
}
