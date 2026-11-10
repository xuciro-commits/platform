package platform

import "strings"

func ListDetailPage(name, title, description string, object AssetRef, list, detail []string, actions []AssetRef) Page {
	fields := func(names []string) []FieldRef {
		out := []FieldRef{}
		for _, name := range names {
			if strings.Contains(name, ".") {
				out = append(out, FieldRef{Path: strings.Split(name, ".")})
			} else {
				out = append(out, FieldRef{Name: name})
			}
		}
		return out
	}
	binding := func(id, widget string, names []string, ports map[string]string) PageDataBinding {
		ports["widget"] = widget
		return PageDataBinding{ID: id, Owner: "binding:" + id, Kind: "record", Object: object, Fields: fields(names), Model: &ModelBinding{Type: object.Name, Fields: fields(names)}, Ports: ports, FieldSlots: map[string][]FieldRef{}}
	}
	return Page{Format: 4, Name: name, Title: title, Description: description, Object: object,
		DataBindings: []PageDataBinding{
			binding("list", "table", list, map[string]string{"parentable": "true", "selectionMode": "write"}),
			binding("detail", "detail", detail, map[string]string{"selectionMode": "read"})},
		ActionTriggers: []PageActionTrigger{{ID: "page", Binding: "page", Actions: actions}, {ID: "list", Binding: "list", Owner: "binding:list"}, {ID: "detail", Binding: "detail", Owner: "binding:detail"}},
		ScopeVisibility: PageScopeVisibility{Root: "page", Owners: map[string]string{"list": "binding:list", "detail": "binding:detail"},
			Scopes: []PageScope{{ID: "page", Kind: "page"}, {ID: "binding:list", Parent: "page", Kind: "page", Binding: "list"}, {ID: "binding:detail", Parent: "page", Kind: "page", Binding: "detail"}}},
		WidgetSpec: PageWidgetSpec{Format: "platform.page.web.v1", Body: []byte(`{"document":null,"layout":"list-detail","sections":[{"binding":"list","id":"list","triggers":["list"]},{"binding":"detail","id":"detail","triggers":["detail"]}]}`)}}
}
