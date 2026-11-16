package pages

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

func CheckApplicationResources(a platform.Application) error {
	if err := a.CheckLanguages(); err != nil {
		return err
	}
	if err := CheckApplicationExtension(a); err != nil {
		return err
	}
	if a.Profile != nil {
		if err := a.Profile.Check(); err != nil {
			return err
		}
	}
	if len(a.Resources) > 1000 {
		return fmt.Errorf("application resources exceed 1000 assets")
	}
	seen := map[platform.AssetRef]bool{}
	for _, ref := range a.Resources {
		if err := ref.Check(); err != nil {
			return err
		}
		if ref.Kind == platform.AssetPage || ref.Kind == platform.AssetApp {
			return fmt.Errorf("application resources use non-navigation assets; pages belong in Pages")
		}
		if seen[ref] {
			return fmt.Errorf("application resource %s is declared twice", ref)
		}
		seen[ref] = true
	}
	return CheckApplicationConcepts(a)
}

func ApplicationDependencies(a platform.Application, owner string) []platform.AssetRef {
	refs := append(slices.Clone(a.Resources), QueryReferences(a.QueryPage())...)
	if a.Extends != nil {
		refs = append(refs, *a.Extends)
	}
	for _, name := range a.Pages {
		refs = append(refs, platform.AssetRef{App: owner, Kind: platform.AssetPage, Name: name})
	}
	slices.SortFunc(refs, platform.CompareAssetRef)
	return slices.Compact(refs)
}

func CheckApplicationExtension(a platform.Application) error {
	if a.Extends == nil {
		if a.Entry != "" || len(a.Replacements) != 0 {
			return fmt.Errorf("an application entry or page replacement needs an extended application")
		}
		return nil
	}
	if err := a.Extends.Check(); err != nil {
		return err
	}
	if a.Extends.Kind != platform.AssetApp {
		return fmt.Errorf("an application can only extend an application")
	}
	if len(a.Replacements) != 0 && strings.TrimSpace(a.Entry) == "" {
		return fmt.Errorf("application page replacements need an explicit entry")
	}
	views := map[string]bool{}
	for _, replacement := range a.Replacements {
		if strings.TrimSpace(replacement.View) == "" || !slices.Contains(a.Pages, replacement.Page) {
			return fmt.Errorf("application replacement %q needs a view and one of its own pages", replacement.View)
		}
		if views[replacement.View] {
			return fmt.Errorf("application view %q is replaced twice", replacement.View)
		}
		views[replacement.View] = true
	}
	return nil
}

func CheckApplicationConcepts(a platform.Application) error {
	if len(a.Concepts) > 100 {
		return fmt.Errorf("application concepts exceed 100 groups")
	}
	ids, assets := map[string]bool{}, map[platform.AssetRef]bool{}
	count := 0
	for _, concept := range a.Concepts {
		if strings.TrimSpace(concept.ID) == "" || len(concept.ID) > 128 || ids[concept.ID] {
			return fmt.Errorf("application concepts need unique nonempty IDs of at most 128 bytes")
		}
		ids[concept.ID] = true
		if strings.TrimSpace(concept.Title) == "" || len(concept.Title) > 512 {
			return fmt.Errorf("application concepts need a title of at most 512 bytes")
		}
		for _, ref := range concept.Assets {
			if err := ref.Check(); err != nil {
				return err
			}
			if ref.Kind == platform.AssetApp || assets[ref] {
				return fmt.Errorf("concept assets must be unique resources, not application roots")
			}
			assets[ref] = true
			count++
		}
	}
	if count > 1000 {
		return fmt.Errorf("application concepts exceed 1000 grouped assets")
	}
	return nil
}
