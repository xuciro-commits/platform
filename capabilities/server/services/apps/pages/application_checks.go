package pages

import (
	"fmt"
	"slices"

	"platformserver/platform"
)

func (b Bindings) CheckApplication(id string, a platform.Application, published func(platform.AssetRef) (bool, error)) error {
	if a.Name == "" || a.Title == "" {
		return fmt.Errorf("application %q: it needs a name and a title", a.Name)
	}
	if len(a.Pages) == 0 && len(a.Resources) == 0 {
		return fmt.Errorf("application %s: it needs pages or declared resources", a.Name)
	}
	if a.Icon != "" && !platform.IconName(a.Icon) {
		return fmt.Errorf("application %s: %q is not an icon name", a.Name, a.Icon)
	}
	for _, page := range a.Pages {
		ref := platform.AssetRef{App: id, Kind: platform.AssetPage, Name: page}
		pageDefinition := slices.IndexFunc(b.Definitions, func(d platform.Definition) bool { return d.Ref == ref && d.Page != nil })
		if pageDefinition >= 0 {
			if err := a.CheckPageVariables(*b.Definitions[pageDefinition].Page); err != nil {
				return err
			}
		}
		if pageDefinition < 0 {
			return fmt.Errorf("application %s: no page %s", a.Name, page)
		}
	}
	if err := a.CheckVariables(); err != nil {
		return err
	}
	if err := b.CheckQueries(a.QueryPage()); err != nil {
		return err
	}
	if err := a.CheckResources(); err != nil {
		return fmt.Errorf("application %s: %w", a.Name, err)
	}
	for _, ref := range a.Resources {
		known, err := published(ref)
		if err != nil {
			return err
		}
		if !known {
			return fmt.Errorf("application %s: no published resource %s", a.Name, ref)
		}
	}
	if err := a.CheckHeader(); err != nil {
		return err
	}
	if err := a.CheckGroups(); err != nil {
		return fmt.Errorf("application %s: %v", a.Name, err)
	}
	return nil
}
