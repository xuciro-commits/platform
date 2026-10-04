package platform

import "testing"

func TestOverlayPresentationKeepsDefaultAndFiniteExplicitRules(t *testing.T) {
	d, s := overlayDocument()
	id := ""
	for key := range d.Overlays {
		id = key
		break
	}
	o := d.Overlays[id]
	o.Presentation = &PageOverlayPresentation{Side: "left", Size: "small", Backdrop: true, CloseOnBackdrop: false, CloseOnEsc: false}
	d.Overlays[id] = o
	d.UIProfile = PageUIProfile()
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PageOverlayPresentation){func(p *PageOverlayPresentation) { p.Side = "top" }, func(p *PageOverlayPresentation) { p.Size = "huge" }, func(p *PageOverlayPresentation) { p.Size = "custom" }, func(p *PageOverlayPresentation) { width := 239; p.Size = "custom"; p.CustomWidth = &width }, func(p *PageOverlayPresentation) { width := 1201; p.Size = "custom"; p.CustomWidth = &width }} {
		copy := *o.Presentation
		change(&copy)
		o.Presentation = &copy
		d.Overlays[id] = o
		if d.Check(s) == nil {
			t.Fatal("invalid overlay presentation accepted")
		}
	}
	o.Presentation = &PageOverlayPresentation{Side: "right", Size: "medium", Backdrop: true, CloseOnBackdrop: true, CloseOnEsc: true}
	d.Overlays[id] = o
	d.UIProfile = "platform.page.v2.90"
	if d.Check(s) == nil {
		t.Fatal("old overlay profile accepted presentation")
	}
	o.Presentation = nil
	d.Overlays[id] = o
	if err := d.Check(s); err != nil {
		t.Fatal("legacy defaults changed", err)
	}
}
