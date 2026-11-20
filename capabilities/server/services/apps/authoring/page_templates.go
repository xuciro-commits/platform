package authoring

import (
	"slices"
	"strings"

	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

func ListDetailPage(name, title, description string, object platform.AssetRef, list, detail []string, actions []platform.AssetRef) platform.Page {
	fields := func(names []string) []platform.FieldRef {
		out := []platform.FieldRef{}
		for _, name := range names {
			if strings.Contains(name, ".") {
				out = append(out, platform.FieldRef{Path: strings.Split(name, ".")})
			} else {
				out = append(out, platform.FieldRef{Name: name})
			}
		}
		return out
	}
	binding := func(id, widget string, names []string, ports map[string]string) platform.PageDataBinding {
		ports["widget"] = widget
		return platform.PageDataBinding{ID: id, Owner: "binding:" + id, Kind: "record", Object: object, Fields: fields(names), Model: &platform.ModelBinding{Type: object.Name, Fields: fields(names)}, Ports: ports, FieldSlots: map[string][]platform.FieldRef{}}
	}
	return platform.Page{Format: 4, Name: name, Title: title, Description: description, Object: object,
		DataBindings: []platform.PageDataBinding{
			binding("list", "table", list, map[string]string{"parentable": "true", "selectionMode": "write"}),
			binding("detail", "detail", detail, map[string]string{"selectionMode": "read"})},
		ActionTriggers: []platform.PageActionTrigger{{ID: "page", Binding: "page", Actions: actions}, {ID: "list", Binding: "list", Owner: "binding:list"}, {ID: "detail", Binding: "detail", Owner: "binding:detail"}},
		ScopeVisibility: platform.PageScopeVisibility{Root: "page", Owners: map[string]string{"list": "binding:list", "detail": "binding:detail"},
			Scopes: []platform.PageScope{{ID: "page", Kind: "page"}, {ID: "binding:list", Parent: "page", Kind: "page", Binding: "list"}, {ID: "binding:detail", Parent: "page", Kind: "page", Binding: "detail"}}},
		WidgetSpec: platform.PageWidgetSpec{Format: "platform.page.web.v1", Body: []byte(`{"document":null,"layout":"list-detail","sections":[{"binding":"list","id":"list","triggers":["list"]},{"binding":"detail","id":"detail","triggers":["detail"]}]}`)}}
}

func CompileObjectPage(o ontology.Object, app string) platform.Page {
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
	typ := o.Type()
	actions := []platform.AssetRef{{App: app, Kind: platform.AssetAction, Name: typ + ".create"}, {App: app, Kind: platform.AssetAction, Name: typ + ".edit"}}
	for _, a := range o.Actions {
		actions = append(actions, platform.AssetRef{App: app, Kind: platform.AssetAction, Name: typ + "." + a.Name})
	}
	title := o.Plural
	if title == "" {
		title = o.Title
	}
	return ListDetailPage(o.PageName(), title, o.Description, platform.AssetRef{App: app, Kind: platform.AssetObject, Name: typ}, list, names, actions)
}
