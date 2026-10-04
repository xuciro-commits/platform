package build

import (
	"fmt"
	"platformserver/platform"
	"slices"
)

// Published page history lives on its original owner record, not a second
// artifact repository. Snapshots themselves contain no recursive history.
func retainPagePublication(p *Page) error {
	snapshots := append(slices.Clone(p.Versions), p.Published, published(*p))
	kept := []string{}
	seen := map[string]bool{}
	for _, raw := range snapshots {
		if raw == "" {
			continue
		}
		saved, ok := wasPublished[Page](raw)
		if !ok || saved.ID != p.ID || saved.Name == "" || saved.Published != "" || len(saved.Versions) != 0 {
			return fmt.Errorf("invalid original page publication history")
		}
		view := descriptor(saved)
		if _, err := platform.PageReleaseAsset(ID, "", view); err != nil {
			return err
		}
		version, err := platform.PageContentVersion(view)
		if err != nil {
			return err
		}
		if !seen[version] {
			seen[version] = true
			kept = append(kept, raw)
		}
	}
	if len(kept) > 64 {
		return fmt.Errorf("a page may retain at most 64 different published contents")
	}
	p.Versions = kept
	p.Published = published(*p)
	return nil
}

// PageContent resolves only an actually published snapshot with an exact
// original descriptor digest. Draft fields and saved candidates are excluded.
func (b *Build) PageContent(name, version string) (platform.Page, bool) {
	if platform.CheckPageContentVersion(version) != nil || b.host == nil {
		return platform.Page{}, false
	}
	rows, err := readDefinitionInventory[Page](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return platform.Page{}, false
	}
	for _, p := range rows {
		if p.Archived || p.Name != name || p.Published == "" {
			continue
		}
		for _, raw := range append(slices.Clone(p.Versions), p.Published) {
			saved, ok := wasPublished[Page](raw)
			if !ok || saved.ID != p.ID || saved.Name == "" || saved.Published != "" || len(saved.Versions) != 0 {
				return platform.Page{}, false
			}
			if saved.Name != name {
				continue
			}
			view := descriptor(saved)
			digest, err := platform.PageContentVersion(view)
			if err == nil && digest == version {
				return view, true
			}
		}
	}
	return platform.Page{}, false
}
