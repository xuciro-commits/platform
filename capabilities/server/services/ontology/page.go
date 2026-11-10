package ontology

import (
	"slices"

	"platformserver/platform"
)

func CompileObjectPage(o Object, app string) platform.Page {
	names := []string{}
	for _, f := range o.Fields {
		names = append(names, f.Name)
	}
	list := slices.Clone(names)
	if len(list) > 4 {
		list = list[:4]
	}
	if len(o.States) > 0 {
		list, names = append(list, "state"), append(names, "state")
	}
	typ := app + "." + o.Name
	actions := []platform.AssetRef{{App: app, Kind: platform.AssetAction, Name: typ + ".create"}, {App: app, Kind: platform.AssetAction, Name: typ + ".edit"}}
	for _, a := range o.Actions {
		actions = append(actions, platform.AssetRef{App: app, Kind: platform.AssetAction, Name: typ + "." + a.Name})
	}
	title := o.Plural
	if title == "" {
		title = o.Title
	}
	return platform.ListDetailPage(o.Name, title, o.Description, platform.AssetRef{App: app, Kind: platform.AssetObject, Name: typ}, list, names, actions)
}
