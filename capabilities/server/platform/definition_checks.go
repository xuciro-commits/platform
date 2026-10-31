package platform

import (
	"fmt"
	"slices"
	"strings"
)

type AppConcept struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Assets []AssetRef `json:"assets"`
}

func (a Application) CheckConcepts() error {
	if len(a.Concepts) > 100 {
		return fmt.Errorf("application concepts exceed 100 groups")
	}
	ids, assets := map[string]bool{}, map[AssetRef]bool{}
	count := 0
	for _, concept := range a.Concepts {
		if strings.TrimSpace(concept.ID) == "" || len(concept.ID) > 128 || ids[concept.ID] {
			return fmt.Errorf("application concepts need unique nonempty IDs of at most 128 bytes")
		}
		ids[concept.ID] = true
		if strings.TrimSpace(concept.Title) == "" || len(concept.Title) > 512 {
			return fmt.Errorf("application concepts need a title of at most 512 bytes")
		}
		for _, ref := range concept.Assets {
			if err := ref.Check(); err != nil {
				return err
			}
			if ref.Kind == AssetApp || assets[ref] {
				return fmt.Errorf("concept assets must be unique resources, not application roots")
			}
			assets[ref] = true
			count++
		}
	}
	if count > 1000 {
		return fmt.Errorf("application concepts exceed 1000 grouped assets")
	}
	return nil
}

func CheckFilterSchema(source PageResourceSource, object EntityInfo) error {
	if source.Object == nil || source.Object.Name != object.Type || source.Object.App != object.App {
		return fmt.Errorf("filter object is unavailable")
	}
	for _, name := range source.Fields {
		field, ok := object.Field(name)
		if !ok || field.Type != "choice" && field.Type != "boolean" && field.Type != "reference" {
			return fmt.Errorf("filter field %s is unavailable or unsupported", name)
		}
	}
	return nil
}

type ApplicationHeader struct {
	Variant   string                  `json:"variant"`
	Title     string                  `json:"title"`
	Logo      string                  `json:"logo,omitempty"`
	Collapsed bool                    `json:"collapsed,omitempty"`
	Items     []ApplicationHeaderItem `json:"items"`
}

type ApplicationHeaderItem struct {
	Kind   string   `json:"kind"`
	Pages  []string `json:"pages,omitempty"`
	Label  string   `json:"label,omitempty"`
	Text   string   `json:"text,omitempty"`
	Action string   `json:"action,omitempty"`
}

func (a Application) CheckHeader() error {
	h := a.Header
	if h == nil {
		return nil
	}
	if !PageUIProfileSupports(a.UIProfile, "platform.page.v2.105") || len(h.Title) > 1024 || len(h.Items) == 0 || len(h.Items) > 24 {
		return fmt.Errorf("application header exceeds its retained profile or content bound")
	}
	if h.Logo != "" && !ValidPageImageURL(h.Logo) {
		return fmt.Errorf("application header logo needs a safe static image URL")
	}
	seen := map[string]bool{}
	for _, item := range h.Items {
		if len(item.Label) > 1024 || len(item.Text) > 1024 || len(item.Pages) > 32 {
			return fmt.Errorf("application header item exceeds its budget")
		}
		for _, name := range item.Pages {
			if !slices.Contains(a.Pages, name) || seen[name] {
				return fmt.Errorf("application header navigation needs unique application pages")
			}
			seen[name] = true
		}
	}
	return nil
}

func (a Application) VisibleHeader() *ApplicationHeader {
	if a.Header == nil {
		return nil
	}
	h := *a.Header
	h.Items = nil
	for _, item := range a.Header.Items {
		item.Pages = slices.DeleteFunc(slices.Clone(item.Pages), func(p string) bool { return !slices.Contains(a.Pages, p) })
		if item.Kind == "tabs" && len(item.Pages) == 0 {
			continue
		}
		h.Items = append(h.Items, item)
	}
	return &h
}

func (v PageVariable) IsWritable() bool { return v.Mode == "state" || v.Mode == "shared" && v.Writable }

func (a Application) CheckVariables() error {
	if a.UIProfile != "" && !SupportsPageUIProfile(a.UIProfile) {
		return fmt.Errorf("unsupported application UI profile %s", a.UIProfile)
	}
	if len(a.Variables) == 0 && len(a.Queries) == 0 {
		return nil
	}
	if !PageUIProfileSupports(a.UIProfile, "platform.page.v2.8") {
		return fmt.Errorf("application variables require UI profile v2.8")
	}
	if (len(a.Queries) > 0 || a.hasResources()) && !PageUIProfileSupports(a.UIProfile, "platform.page.v2.12") {
		return fmt.Errorf("application query resources require v2.12")
	}
	for id, v := range a.Variables {
		if v.Mode == "property" && !PageUIProfileSupports(a.UIProfile, "platform.page.v2.17") {
			return fmt.Errorf("application property %s requires v2.17", id)
		}
		if v.Type == "decimal" && !PageUIProfileSupports(a.UIProfile, "platform.page.v2.16") {
			return fmt.Errorf("application decimal %s requires v2.16", id)
		}
		if v.Type == "filter" && !PageUIProfileSupports(a.UIProfile, "platform.page.v2.15") {
			return fmt.Errorf("application filter %s requires v2.15", id)
		}
		if v.Type == "record-set" && !PageUIProfileSupports(a.UIProfile, pageWidgets.Runtime.RecordSelection.SharedUIProfile) {
			return fmt.Errorf("application record-set %s requires its shared profile", id)
		}
		if v.Type == "record" && !PageUIProfileSupports(a.UIProfile, "platform.page.v2.14") {
			return fmt.Errorf("application record %s requires v2.14", id)
		}
		if v.Scope != "application" || !slices.Contains(pageWidgets.Runtime.Application.Modes, v.Mode) {
			return fmt.Errorf("application variable %s needs its own pure scalar declaration", id)
		}
	}
	d := a.QueryPage().InputSchema
	if err := d.CheckApplicationQueries(); err != nil {
		return err
	}
	return d.CheckVariables()
}

func (a Application) CheckPageVariables(p Page) error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	for id, v := range safe.InputSchema.Variables {
		if v.Scope != "application" {
			continue
		}
		if v.Source == nil {
			return fmt.Errorf("page %s shared binding %s has no source", p.Name, id)
		}
		source, ok := a.Variables[v.Source.Variable]
		matches := true
		if v.Type == "object-set" {
			matches = source.Mode == "resource" && source.Source != nil && source.Source.Kind == "plan" && v.Source.Object != nil && a.Queries[source.Source.Query].Object == *v.Source.Object
		}
		if v.Type == "record" || v.Type == "filter" || v.Type == "record-set" {
			matches = source.Mode == "resource" && source.Source != nil && source.Source.Kind == v.Type && source.Source.Object != nil && v.Source.Object != nil && *source.Source.Object == *v.Source.Object
		}
		if !ok || !matches || source.Type != v.Type || v.Writable && source.Mode != "state" && v.Type != "record" && v.Type != "filter" && v.Type != "record-set" {
			return fmt.Errorf("application %s does not satisfy page %s shared binding %s", a.Name, p.Name, id)
		}
		for _, binding := range safe.DataBindings {
			if binding.Ports["filterVariable"] != id {
				continue
			}
			for _, field := range binding.Fields {
				if source.Source == nil || !slices.Contains(source.Source.Fields, field.Key()) {
					return fmt.Errorf("page filter field is not allowed by application")
				}
			}
		}

	}
	return nil
}

func (a Application) QueryPage() Page {
	return Page{Format: 3, InputSchema: PageInputSchema{profile: a.UIProfile, Variables: a.Variables, Queries: a.Queries}}
}

func (a Application) hasResources() bool {
	for _, v := range a.Variables {
		if v.Mode == "resource" {
			return true
		}
	}
	return false
}
