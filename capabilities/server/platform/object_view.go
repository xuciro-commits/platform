package platform

import "fmt"

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
