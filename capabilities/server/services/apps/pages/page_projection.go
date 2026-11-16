package pages

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
)

func Safety(p platform.Page) (platform.Page, error) {
	if p.Format != 4 {
		return platform.Page{}, fmt.Errorf("unsupported page format %d", p.Format)
	}
	if p.RetainedVersion == "" {
		version, err := PageContentVersion(p)
		if err != nil {
			return platform.Page{}, err
		}
		p.RetainedVersion = version
	}
	return p, nil
}

func PageContentVersion(page platform.Page) (string, error) {
	if page.RetainedVersion != "" {
		return page.RetainedVersion, nil
	}
	page.PresentationWords = nil
	body, err := json.Marshal(page)
	if err != nil {
		return "", err
	}
	body, err = platform.CanonicalAssetBody(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return "page.sha256." + hex.EncodeToString(sum[:]), nil
}

func CheckPageVariables(a platform.Application, p platform.Page) error {
	safe, err := Safety(p)
	if err != nil {
		return err
	}
	for id, v := range safe.InputSchema.Variables {
		if v.Scope != "application" {
			continue
		}
		if v.Source == nil {
			return fmt.Errorf("page %s shared binding %s has no source", p.Name, id)
		}
		source, ok := a.Variables[v.Source.Variable]
		matches := true
		if v.Type == "object-set" {
			matches = source.Mode == "resource" && source.Source != nil && source.Source.Kind == "plan" && v.Source.Object != nil && a.Queries[source.Source.Query].Object == *v.Source.Object
		}
		if v.Type == "record" || v.Type == "filter" || v.Type == "record-set" {
			matches = source.Mode == "resource" && source.Source != nil && source.Source.Kind == v.Type && source.Source.Object != nil && v.Source.Object != nil && *source.Source.Object == *v.Source.Object
		}
		if !ok || !matches || source.Type != v.Type || v.Writable && source.Mode != "state" && v.Type != "record" && v.Type != "filter" && v.Type != "record-set" {
			return fmt.Errorf("application %s does not satisfy page %s shared binding %s", a.Name, p.Name, id)
		}
		for _, binding := range safe.DataBindings {
			if binding.Ports["filterVariable"] != id {
				continue
			}
			for _, field := range binding.Fields {
				if source.Source == nil || !slices.Contains(source.Source.Fields, field.Key()) {
					return fmt.Errorf("page filter field is not allowed by application")
				}
			}
		}

	}
	return nil
}

func recordVariableInterface(p platform.Page, variable string) string {
	safe, err := Safety(p)
	if err != nil {
		return ""
	}
	v := safe.InputSchema.Variables[variable]
	if v.Source != nil {
		for _, binding := range safe.DataBindings {
			if binding.ID == v.Source.Section {
				return safe.WindowVariableInterface(binding.Ports["collectionVariable"])
			}
		}
	}
	return ""
}

func checkRecordPorts(p platform.Page) error {
	safe, err := Safety(p)
	if err != nil {
		return err
	}
	selections := map[string]platform.AssetRef{}
	for _, selection := range safe.InputSchema.Selections {
		if !platform.PageNodeID.MatchString(selection.Name) || selections[selection.Name].Name != "" || selection.Object.Check() != nil {
			return fmt.Errorf("page selections need unique typed record names")
		}
		selections[selection.Name] = selection.Object
	}
	for _, binding := range safe.DataBindings {
		if selected := binding.Ports["selection"]; selected != "" {
			if selections[selected] != binding.ObjectRef(safe) {
				return fmt.Errorf("selection %s does not hold %s", selected, binding.ObjectRef(safe).Name)
			}
		}
		if parent := binding.Ports["parentSelection"]; parent != "" {
			if selections[parent].Name == "" {
				return fmt.Errorf("parent selection %s does not hold a record", parent)
			}
		}
	}
	for _, binding := range safe.DataBindings {
		variable := binding.Ports["recordVariable"]
		producerBound := slices.Contains(recordProducerWidgets, binding.Ports["widget"]) || safe.InputSchema.Variables[variable].Source != nil && safe.InputSchema.Variables[variable].Source.Port != "" && binding.Ports["widget"] != "observation"
		if variable != "" && producerBound && recordVariableInterface(safe, variable) == "" {
			object := safe.RecordResourceObject(variable)
			if object.Name == "" || object != binding.ObjectRef(safe) {
				return fmt.Errorf("page record binding differs from its original producer")
			}
		}
	}
	for _, binding := range safe.DataBindings {
		variable := binding.Ports["recordVariable"]
		if binding.Ports["widget"] != "observation" || variable == "" {
			continue
		}
		for _, resource := range binding.Resources {
			if resource.Kind == "observation" && resource.Source == "statistics" && (resource.Object == nil || safe.RecordResourceObject(variable) != *resource.Object) {
				return fmt.Errorf("statistics context differs from its original full asset identity")
			}
		}
	}
	if safe.InputSchema.Interface != nil {
		for _, port := range safe.InputSchema.Interface.Outputs {
			if port.Type == "record" && (port.Object == nil || safe.RecordVariableObject(port.Variable) != port.Object.Name) {
				return fmt.Errorf("page output differs from original record object")
			}
		}
	}
	return nil
}

var recordProducerWidgets = []string{"record-card", "breadcrumb", "graph-explorer", "vertex-graph", "record-comments", "record-uploader", "media-preview", "pdf-viewer", "image-annotation", "scene-3d"}
