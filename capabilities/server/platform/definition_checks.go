package platform

import (
	"fmt"
	"slices"
)

type AppConcept struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Assets []AssetRef `json:"assets"`
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
		if v.Type == "record-set" && !PageUIProfileSupports(a.UIProfile, PageWidgets.Runtime.RecordSelection.SharedUIProfile) {
			return fmt.Errorf("application record-set %s requires its shared profile", id)
		}
		if v.Type == "record" && !PageUIProfileSupports(a.UIProfile, "platform.page.v2.14") {
			return fmt.Errorf("application record %s requires v2.14", id)
		}
		if v.Scope != "application" || !slices.Contains(PageWidgets.Runtime.Application.Modes, v.Mode) {
			return fmt.Errorf("application variable %s needs its own pure scalar declaration", id)
		}
	}
	d := a.QueryPage().InputSchema
	if err := d.CheckApplicationQueries(); err != nil {
		return err
	}
	return d.CheckVariables()
}

func (a Application) QueryPage() Page {
	return Page{Format: 4, InputSchema: PageInputSchema{profile: a.UIProfile, Variables: a.Variables, Queries: a.Queries}}
}

func (a Application) hasResources() bool {
	for _, v := range a.Variables {
		if v.Mode == "resource" {
			return true
		}
	}
	return false
}
