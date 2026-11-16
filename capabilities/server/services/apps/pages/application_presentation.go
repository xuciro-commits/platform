package pages

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

func CheckApplicationGroups(a platform.Application) error {
	grouped := map[string]string{}
	for _, g := range a.Groups {
		if strings.TrimSpace(g.Title) == "" {
			return fmt.Errorf("a group needs a title")
		}
		if len(g.Pages) == 0 {
			return fmt.Errorf("the group %q holds no page", g.Title)
		}
		for _, page := range g.Pages {
			if !slices.Contains(a.Pages, page) {
				return fmt.Errorf("the group %q names the page %q, which the application does not hold", g.Title, page)
			}
			if other, ok := grouped[page]; ok {
				return fmt.Errorf("the page %q is under %q and %q", page, other, g.Title)
			}
			grouped[page] = g.Title
		}
	}
	return nil
}

func CheckApplicationHeader(a platform.Application) error {
	h := a.Header
	if h == nil {
		return nil
	}
	if !platform.PageUIProfileSupports(a.UIProfile, "platform.page.v2.105") || len(h.Title) > 1024 || len(h.Items) == 0 || len(h.Items) > 24 {
		return fmt.Errorf("application header exceeds its retained profile or content bound")
	}
	if h.Logo != "" && !validPageImageURL(h.Logo) {
		return fmt.Errorf("application header logo needs a safe static image URL")
	}
	seen := map[string]bool{}
	for _, item := range h.Items {
		if len(item.Label) > 1024 || len(item.Text) > 1024 || len(item.Pages) > 32 {
			return fmt.Errorf("application header item exceeds its budget")
		}
		for _, name := range item.Pages {
			if !slices.Contains(a.Pages, name) || seen[name] {
				return fmt.Errorf("application header navigation needs unique application pages")
			}
			seen[name] = true
		}
	}
	return nil
}

func VisibleApplicationHeader(a platform.Application) *platform.ApplicationHeader {
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
