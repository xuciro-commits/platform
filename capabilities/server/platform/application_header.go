package platform

import (
	"fmt"
	"slices"
)

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
	if !PageUIProfileSupports(a.UIProfile, "platform.page.v2.105") || !slices.Contains([]string{"horizontal", "vertical"}, h.Variant) || len(h.Title) > 1024 || len(h.Items) > 24 || len(h.Items) == 0 || h.Collapsed && h.Variant != "vertical" {
		return fmt.Errorf("application header needs its profile, bounded items and a supported layout")
	}
	if h.Logo != "" {
		if !ValidPageImageURL(h.Logo) {
			return fmt.Errorf("application header logo needs a safe static image URL")
		}
	}
	seen := map[string]bool{}
	for _, item := range h.Items {
		if len(item.Label) > 1024 || len(item.Text) > 1024 || len(item.Pages) > 32 {
			return fmt.Errorf("application header item exceeds its budget")
		}
		switch item.Kind {
		case "logo", "title", "spacer":
			if len(item.Pages) > 0 || item.Label != "" || item.Text != "" || item.Action != "" {
				return fmt.Errorf("application header item has incompatible properties")
			}
		case "tabs":
			if item.Label != "" || item.Text != "" || item.Action != "" || len(item.Pages) == 0 {
				return fmt.Errorf("application header navigation needs pages")
			}
			for _, p := range item.Pages {
				if !slices.Contains(a.Pages, p) || seen[p] {
					return fmt.Errorf("application header navigation needs unique application pages")
				}
				seen[p] = true
			}
		case "text":
			if len(item.Pages) > 0 || item.Label != "" || item.Action != "" {
				return fmt.Errorf("application header text has incompatible properties")
			}
		case "button":
			if len(item.Pages) > 0 || item.Label == "" || item.Text != "" || !slices.Contains([]string{"refresh", "theme"}, item.Action) {
				return fmt.Errorf("application header button needs a declared read or appearance action")
			}
		default:
			return fmt.Errorf("application header item is unsupported")
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
