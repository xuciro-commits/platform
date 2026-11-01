package platform

import (
	"encoding/json"
	"fmt"
	"slices"
)

type ObjectView struct {
	Application AssetRef `json:"application"`
	Page        AssetRef `json:"page"`
}

func (v ObjectView) Check() error {
	if v.Application.Kind != AssetApp || v.Application.Check() != nil || v.Page.Kind != AssetPage || v.Page.Check() != nil || v.Page.App != v.Application.App {
		return fmt.Errorf("Object view needs a valid module and a page from that module")
	}
	return nil
}

func checkObjectView(object AssetRef, view ObjectView, assets map[AssetRef]ReleaseAsset) error {
	if err := view.Check(); err != nil {
		return err
	}
	var app Application
	if asset, ok := assets[view.Application]; !ok || json.Unmarshal(asset.Body, &app) != nil || !slices.Contains(app.Pages, view.Page.Name) {
		return fmt.Errorf("Object view page must belong to its saved module")
	}
	var page Page
	if asset, ok := assets[view.Page]; !ok || json.Unmarshal(asset.Body, &page) != nil || page.Object != object {
		return fmt.Errorf("Object view page must read its bound object type")
	}
	schema := page.inputSchema()
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
