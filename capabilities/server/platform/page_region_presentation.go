package platform

import "fmt"

// PageRegionPresentation is bounded container chrome; it owns no business state.
type PageRegionPresentation struct {
	Padding          *int   `json:"padding,omitempty"`
	Background       string `json:"background,omitempty"`
	Border           bool   `json:"border,omitempty"`
	ShowHeader       bool   `json:"showHeader,omitempty"`
	Collapsible      bool   `json:"collapsible,omitempty"`
	DefaultCollapsed bool   `json:"defaultCollapsed,omitempty"`
}

func (d *PageDocument) checkRegionPresentation(id string, n PageLayoutNode) error {
	p := n.Presentation
	if p == nil {
		return nil
	}
	l := pageWidgets.Layout
	if !PageUIProfileSupports(d.UIProfile, l.PresentationProfile) || n.Kind != "rows" && n.Kind != "columns" || p.Padding != nil && (*p.Padding < 0 || *p.Padding > l.MaxPadding) || p.Background != "" && p.Background != "default" && p.Background != "panel" || p.ShowHeader && n.Title == "" || p.Collapsible && (!p.ShowHeader || n.Title == "") || p.DefaultCollapsed && !p.Collapsible {
		return fmt.Errorf("page node %s: invalid region presentation", id)
	}
	return nil
}
