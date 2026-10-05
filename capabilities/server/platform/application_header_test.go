package platform

import "testing"

func headerApplication() Application {
	return Application{Name: "desk", Title: "Desk", UIProfile: PageUIProfile(), Pages: []string{"one", "two"}, Header: &ApplicationHeader{Variant: "horizontal", Title: "Original", Items: []ApplicationHeaderItem{{Kind: "logo"}, {Kind: "title"}, {Kind: "tabs", Pages: []string{"two", "one"}}, {Kind: "button", Label: "Refresh", Action: "refresh"}, {Kind: "button", Label: "Theme", Action: "theme"}}}}
}

func TestApplicationHeaderContractAndProjection(t *testing.T) {
	a := headerApplication()
	if err := a.CheckHeader(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Application){func(a *Application) { a.UIProfile = "platform.page.v2.104" }, func(a *Application) { a.Header.Variant = "unknown" }, func(a *Application) { a.Header.Collapsed = true }, func(a *Application) { a.Header.Logo = "javascript:alert(1)" }, func(a *Application) { a.Header.Items[2].Pages = []string{"missing"} }, func(a *Application) { a.Header.Items[2].Pages = []string{"one", "one"} }, func(a *Application) { a.Header.Items[3].Action = "execute" }} {
		next := headerApplication()
		change(&next)
		if err := next.CheckHeader(); err == nil {
			t.Fatal("invalid application header was accepted")
		}
	}
	a.Pages = []string{"one"}
	projected := a.VisibleHeader()
	if len(projected.Items[2].Pages) != 1 || projected.Items[2].Pages[0] != "one" {
		t.Fatal("hidden page remained in header")
	}
	if len(a.Header.Items[2].Pages) != 2 {
		t.Fatal("projection mutated original header")
	}
}
