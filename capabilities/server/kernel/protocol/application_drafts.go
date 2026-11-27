package protocol

import (
	"encoding/json"

	"platformserver/platform"
)

const (
	PageType      = "build.page"
	SchemaRelease = PageType + ".publish"
)

type Page struct {
	platform.Record
	Format          int                          `json:"format,omitempty"`
	DataBindings    []platform.PageDataBinding   `json:"dataBindings,omitempty" type:"json"`
	ActionTriggers  []platform.PageActionTrigger `json:"actionTriggers,omitempty" type:"json"`
	InputSchema     platform.PageInputSchema     `json:"inputSchema,omitzero" type:"json"`
	ScopeVisibility platform.PageScopeVisibility `json:"scopeVisibility,omitzero" type:"json"`
	WidgetSpec      platform.PageWidgetSpec      `json:"widgetSpec,omitzero" type:"json"`
	Name            string                       `json:"name" field:"required,search" title:"API name" help:"Its name in the platform, lower-case letters and digits" example:"visits"`
	Title           string                       `json:"title" field:"required,search" title:"Name" help:"What people call it" example:"Visits"`
	Description     string                       `json:"description,omitempty" type:"longtext" help:"What people do on this page"`
	Object          string                       `json:"object" field:"required" title:"Source object" help:"Object it shows. An object this tenant has: one it defined, or an app's own" example:"build.visits"`
	State           string                       `json:"state" field:"readonly" choices:"draft,published"`
	Published       string                       `json:"published,omitempty" field:"readonly,internal" type:"longtext" title:"Installed" help:"What is installed"`
	Versions        []string                     `json:"versions,omitempty" field:"readonly" type:"lines" title:"Published contents" help:"Published page contents"`
}

const (
	AppType        = "build.app"
	SchemaHandOver = AppType + ".publish"
)

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
	Name         string                                `json:"name" field:"required,search" title:"API name" help:"Its name in the platform, lower-case letters and digits" example:"frontdesk"`
	Title        string                                `json:"title" field:"required,search" title:"Name" help:"What people call it" example:"Front desk"`
	Description  string                                `json:"description,omitempty" type:"longtext" help:"What people do in it"`
	Icon         string                                `json:"icon,omitempty" help:"How it is drawn in the launcher"`
	Surface      string                                `json:"surface,omitempty" field:"aside" choices:"work,master-data" title:"Where it appears" help:"work puts it in the application rail; master-data puts it under Admin → Master data"`
	Pages        []string                              `json:"pages" title:"Pages" help:"The pages it holds, by their name, in the order people see them"`
	Resources    []platform.AssetRef                   `json:"resources,omitempty" field:"aside" title:"Application resources" help:"Published objects, workflows and functions included in its release; ownership and permissions stay with each resource"`
	Concepts     []platform.AppConcept                 `json:"concepts,omitempty" field:"aside" type:"json" title:"Business concepts" help:"Named studio groups; grouping changes navigation only"`
	Groups       []Group                               `json:"groups,omitempty" title:"Groups" help:"Headings in its navigation, each over some of its pages in their order"`
	Dashboard    []DashboardBlock                      `json:"dashboard,omitempty" field:"aside" type:"json" title:"Dashboard" help:"The blocks of its home page: numbers and charts over its object types, each opening the matching list"`
	State        string                                `json:"state" field:"readonly" choices:"draft,published,withdrawn"`
	Published    string                                `json:"published,omitempty" field:"readonly,internal" type:"longtext" title:"Installed" help:"What is installed"`
}

type DashboardBlock struct {
	Title   string          `json:"title"`
	Kind    string          `json:"kind"`
	Object  string          `json:"object"`
	Domain  json.RawMessage `json:"domain,omitempty"`
	Measure string          `json:"measure,omitempty"`
	Group   string          `json:"group,omitempty"`
}

type AppMenuEntry struct {
	Kind   string `json:"kind,omitempty"`
	ID     string `json:"id,omitempty"`
	Title  string `json:"title,omitempty"`
	Body   string `json:"body,omitempty"`
	Page   string `json:"page,omitempty"`
	Object string `json:"object,omitempty"`
	Report string `json:"report,omitempty"`
}

type Group struct {
	ID        string         `json:"id,omitempty" title:"Identifier"`
	Title     string         `json:"title" field:"required" title:"Heading"`
	Roles     []string       `json:"roles,omitempty" title:"Roles" help:"Only members holding one of these roles see the group; empty for everyone"`
	Pages     []string       `json:"pages,omitempty" title:"Pages" help:"Pages of the application, by their name, in their order"`
	Entries   []AppMenuEntry `json:"entries,omitempty" title:"Entries"`
	Groups    []Group        `json:"groups,omitempty" title:"Subgroups"`
	Collapsed bool           `json:"collapsed,omitempty" title:"Collapsed by default"`
	Layout    string         `json:"layout,omitempty" choices:"menu,settings" title:"Layout" help:"menu lists each page; settings shows the group as one page with a table per object"`
}
