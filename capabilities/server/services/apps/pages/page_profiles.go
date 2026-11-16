package pages

import (
	"fmt"

	"platformserver/platform"
)

func checkProfile(d platform.PageInputSchema) error {
	for id, v := range d.Variables {
		if err := checkVariableProfile(d, id, v); err != nil {
			return err
		}
	}
	return nil
}

func checkVariableProfile(d platform.PageInputSchema, id string, v platform.PageVariable) error {
	supports := func(feature string) bool {
		return pageUIProfileSupports(profileVersion(d), pageWidgets.Features[feature])
	}
	switch {
	case v.Mode == "property" && !supports("propertyVariables"):
		return fmt.Errorf("property variable %s requires v2.17", id)
	case v.Type == "decimal" && !supports("decimalVariables"):
		return fmt.Errorf("decimal variable %s requires v2.16", id)
	case v.Scope == "application" && v.Type == "filter" && !supports("applicationFilters"):
		return fmt.Errorf("shared filter %s requires v2.15", id)
	case v.Scope == "application" && v.Type == "record" && !supports("applicationRecords"):
		return fmt.Errorf("shared record %s requires v2.14", id)
	case v.Scope == "application" && v.Type == "object-set" && !supports("applicationCollections"):
		return fmt.Errorf("shared window %s requires v2.12", id)
	case v.Scope == "application" && (!supports("applicationBindings") || v.Mode != "shared"):
		return fmt.Errorf("page variable %s needs v2.8 and a shared application binding", id)
	case v.Scope == "overlay" && v.Type == "filter" && !supports("overlayFilters"):
		return fmt.Errorf("overlay filter %s requires v2.13", id)
	case v.Scope == "overlay" && v.Mode == "resource" && !supports("overlayResources"):
		return fmt.Errorf("overlay resource %s requires UI profile v2.11", id)
	case v.Scope == "overlay" && !supports("overlayVariables"):
		return fmt.Errorf("page variable %s requires UI profile v2.7", id)
	case v.Scope == pageWidgets.Runtime.Loop.Scope && !supports("loopVariables"):
		return fmt.Errorf("page variable %s requires UI profile v2.5", id)
	case v.Mode == "resource" && !supports("resourceVariables"):
		return fmt.Errorf("page variable %s requires UI profile v2.3", id)
	}
	return nil
}
