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
	if len(a.Pages) == 0 && len(a.Groups) == 0 && len(a.Resources) == 0 {
		return fmt.Errorf("application %s: it needs pages or declared resources", a.Name)
	}
	if a.Icon != "" && !platform.IconName(a.Icon) {
		return fmt.Errorf("application %s: %q is not an icon name", a.Name, a.Icon)
	}
	if !slices.Contains([]string{"", "work", "master-data"}, a.Surface) {
		return fmt.Errorf("application %s: surface %q is not work or master-data", a.Name, a.Surface)
	}
	for _, page := range a.Pages {
		ref := platform.AssetRef{App: id, Kind: platform.AssetPage, Name: page}
		pageDefinition := slices.IndexFunc(b.Definitions, func(d platform.Definition) bool { return d.Ref == ref && d.Page != nil })
		if pageDefinition >= 0 {
			if err := CheckPageVariables(a, *b.Definitions[pageDefinition].Page); err != nil {
				return err
			}
		}
		if pageDefinition < 0 {
			return fmt.Errorf("application %s: no page %s", a.Name, page)
		}
	}
	if err := CheckApplicationVariables(a); err != nil {
		return err
	}
	if err := b.CheckQueries(ApplicationPage(a)); err != nil {
		return err
	}
	if err := CheckApplicationResources(a); err != nil {
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
	if err := CheckApplicationHeader(a); err != nil {
		return err
	}
	if err := CheckApplicationGroups(a); err != nil {
		return fmt.Errorf("application %s: %v", a.Name, err)
	}
	return nil
}
