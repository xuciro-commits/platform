package publication

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
)

func checkObjectView(object platform.AssetRef, view platform.ObjectView, assets map[platform.AssetRef]platform.ReleaseAsset) error {
	if err := view.Check(); err != nil {
		return err
	}
	var app platform.Application
	if asset, ok := assets[view.Application]; !ok || json.Unmarshal(asset.Body, &app) != nil || !slices.Contains(app.Pages, view.Page.Name) {
		return fmt.Errorf("Object view page must belong to its saved module")
	}
	var page platform.Page
	if asset, ok := assets[view.Page]; !ok || json.Unmarshal(asset.Body, &page) != nil || page.Object != object {
		return fmt.Errorf("Object view page must read its bound object type")
	}
	schema := page.InputSchema
	if schema.Interface == nil {
		return fmt.Errorf("Object view page needs a required currentObject record input for its bound object type")
	}
	input, ok := schema.Interface.Inputs["currentObject"]
	if !ok || input.Type != "record" || !input.Required || input.Object == nil || *input.Object != object {
		return fmt.Errorf("Object view page needs a required currentObject record input for its bound object type")
	}
	for name, port := range schema.Interface.Inputs {
		if name != "currentObject" && port.Required {
			return fmt.Errorf("Object view page cannot require inputs other than currentObject")
		}
	}
	return nil
}
