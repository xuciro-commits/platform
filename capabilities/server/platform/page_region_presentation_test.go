package platform

import (
	"encoding/json"
	"testing"
)

func TestOriginalRegionPresentation(t *testing.T) {
	makePage := func() (*PageDocument, []Section) {
		d, s := nestedDocument()
		padding := 10
		n := d.Nodes["columns"]
		n.Title = "Detail"
		n.Presentation = &PageRegionPresentation{Padding: &padding, Background: "panel", Border: true, ShowHeader: true, Collapsible: true}
		d.Nodes["columns"] = n
		return d, s
	}
	d, s := makePage()
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var restored PageDocument
	if err = json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if err = restored.Check(s); err != nil {
		t.Fatal(err)
	}
	if !restored.Nodes["columns"].Presentation.Collapsible || *restored.Nodes["columns"].Presentation.Padding != 10 {
		t.Fatal("lost original presentation")
	}
	for _, change := range []func(*PageDocument){func(d *PageDocument) { d.UIProfile = "platform.page.v2.92" }, func(d *PageDocument) { *d.Nodes["columns"].Presentation.Padding = 65 }, func(d *PageDocument) { *d.Nodes["columns"].Presentation.Padding = -1 }, func(d *PageDocument) { d.Nodes["columns"].Presentation.Background = "script" }, func(d *PageDocument) { d.Nodes["columns"].Presentation.ShowHeader = false }, func(d *PageDocument) { n := d.Nodes["columns"]; n.Title = ""; d.Nodes["columns"] = n }, func(d *PageDocument) {
		p := d.Nodes["columns"].Presentation
		p.Collapsible = false
		p.DefaultCollapsed = true
	}, func(d *PageDocument) {
		n := d.Nodes["table"]
		n.Presentation = d.Nodes["columns"].Presentation
		d.Nodes["table"] = n
	}} {
		d, s := makePage()
		change(d)
		if err := d.Check(s); err == nil {
			t.Fatal("accepted invalid region")
		}
	}
	d, s = makePage()
	*d.Nodes["columns"].Presentation.Padding = 0
	if err := d.Check(s); err != nil {
		t.Fatal("lost explicit zero", err)
	}
}
