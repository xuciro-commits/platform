package pages

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"platformserver/kernel/protocol"
	"platformserver/platform"
)

func CheckApplicationGroups(a platform.Application) error {
	grouped := map[string]string{}
	groupIDs := map[string]bool{}
	var check func(g platform.AppGroup, depth int) error
	check = func(g platform.AppGroup, depth int) error {
		if strings.TrimSpace(g.Title) == "" {
			return fmt.Errorf("a group needs a title")
		}
		if g.ID != "" {
			if groupIDs[g.ID] {
				return fmt.Errorf("the group id %q is used twice", g.ID)
			}
			groupIDs[g.ID] = true
		}
		if depth > 1 && len(g.Groups) > 0 {
			return fmt.Errorf("the group %q nests more than two levels", g.Title)
		}
		var entries []protocol.AppMenuEntry
		if len(g.Entries) > 0 && json.Unmarshal(g.Entries, &entries) != nil {
			return fmt.Errorf("the group %q has invalid menu entries", g.Title)
		}
		if len(g.Pages) == 0 && len(entries) == 0 && len(g.Groups) == 0 {
			return fmt.Errorf("the group %q holds no page", g.Title)
		}
		checkPage := func(page string) error {
			if !slices.Contains(a.Pages, page) {
				return fmt.Errorf("the group %q names the page %q, which the application does not hold", g.Title, page)
			}
			if other, ok := grouped[page]; ok {
				return fmt.Errorf("the page %q is under %q and %q", page, other, g.Title)
			}
			grouped[page] = g.Title
			return nil
		}
		for _, page := range g.Pages {
			if err := checkPage(page); err != nil {
				return err
			}
		}
		for _, entry := range entries {
			switch entry.Kind {
			case "", "page":
				if err := checkPage(entry.Page); err != nil {
					return err
				}
			case "note":
				if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.Title) == "" {
					return fmt.Errorf("a note entry in %q needs an id and a title", g.Title)
				}
			case "dashboard":
			case "report":
				if strings.TrimSpace(entry.Object) == "" || strings.TrimSpace(entry.Report) == "" {
					return fmt.Errorf("a report entry in %q needs an object and a report name", g.Title)
				}
			default:
				return fmt.Errorf("unknown menu entry kind %q in %q", entry.Kind, g.Title)
			}
		}
		for _, sub := range g.Groups {
			if err := check(sub, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, g := range a.Groups {
		if err := check(g, 1); err != nil {
			return err
		}
	}
	return nil
}

func CheckApplicationDashboard(a platform.Application) error {
	var blocks []protocol.DashboardBlock
	if len(a.Dashboard) > 0 && json.Unmarshal(a.Dashboard, &blocks) != nil {
		return fmt.Errorf("the dashboard is a list of blocks")
	}
	if len(blocks) > 24 {
		return fmt.Errorf("a dashboard holds at most 24 blocks")
	}
	titles := map[string]bool{}
	for _, block := range blocks {
		if strings.TrimSpace(block.Title) == "" || titles[block.Title] {
			return fmt.Errorf("each dashboard block needs its own title")
		}
		titles[block.Title] = true
		if block.Object == "" {
			return fmt.Errorf("the dashboard block %q names no object type", block.Title)
		}
		if !slices.Contains([]string{"metric", "funnel", "share", "bar", "line"}, block.Kind) {
			return fmt.Errorf("the dashboard block %q is a %q; a block is a metric, funnel, share, bar or line", block.Title, block.Kind)
		}
		if (block.Kind == "metric") != (block.Group == "") {
			return fmt.Errorf("the dashboard block %q: a metric has no group, a chart needs one", block.Title)
		}
	}
	return nil
}

func CheckApplicationHeader(a platform.Application) error {
	h := a.Header
	if h == nil {
		return nil
	}
	if !pageUIProfileSupports(a.UIProfile, "platform.page.v2.105") || len(h.Title) > 1024 || len(h.Items) == 0 || len(h.Items) > 24 {
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
