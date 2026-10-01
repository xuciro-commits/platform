package platform

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
)

// The application API owns this build-time UI contract (ADR-0046). The same
// bytes generate the browser registry; they describe presentation, not grants
// or new business execution capabilities.
//
//go:embed pageui/widgets.json
var pageWidgetJSON []byte

type pageWidgetContract struct {
	ComponentID       string         `json:"componentID"`
	ConfigVersion     int            `json:"configVersion"`
	RequiredUIProfile string         `json:"requiredUIProfile"`
	PropsSchema       ValueSchema    `json:"propsSchema"`
	Defaults          map[string]any `json:"defaults"`
}

type pageUIContract struct {
	UIProfile         string               `json:"uiProfile"`
	SupportedProfiles []string             `json:"supportedProfiles"`
	Widgets           []pageWidgetContract `json:"widgets"`
	Runtime           pageRuntimeContract  `json:"runtime"`
}

var pageWidgets = func() pageUIContract {
	var manifest pageUIContract
	if err := json.Unmarshal(pageWidgetJSON, &manifest); err != nil {
		panic(err)
	}
	seen := map[string]bool{}
	for _, widget := range manifest.Widgets {
		if widget.ComponentID == "" || seen[widget.ComponentID] || widget.ConfigVersion < 1 || !slices.Contains(manifest.SupportedProfiles, widget.RequiredUIProfile) {
			panic("invalid page widget contract identity")
		}
		seen[widget.ComponentID] = true
		if err := widget.PropsSchema.Check(); err != nil {
			panic(err)
		}
		defaults, _ := json.Marshal(widget.Defaults)
		if err := widget.PropsSchema.Validate(defaults, 1<<20); err != nil {
			panic(err)
		}
	}
	return manifest
}()

// PageUIManifest returns the exact descriptor used by api-types and Catalog.
func PageUIManifest() string { return string(pageWidgetJSON) }
func PageUIProfile() string  { return pageWidgets.UIProfile }

func SupportsPageUIProfile(profile string) bool {
	return slices.Contains(pageWidgets.SupportedProfiles, profile)
}

func PageUIProfileSupports(profile, required string) bool {
	current, minimum := slices.Index(pageWidgets.SupportedProfiles, profile), slices.Index(pageWidgets.SupportedProfiles, required)
	return current >= 0 && minimum >= 0 && current >= minimum
}

// Widgets remains the original public discovery list, derived from the one
// descriptor rather than a separately maintained enumeration.
var Widgets = func() []string {
	ids := make([]string, 0, len(pageWidgets.Widgets))
	for _, widget := range pageWidgets.Widgets {
		ids = append(ids, widget.ComponentID)
	}
	return ids
}()

func checkPageWidget(section Section) error {
	for _, widget := range pageWidgets.Widgets {
		if widget.ComponentID != section.Widget {
			continue
		}
		if section.ConfigVersion != widget.ConfigVersion {
			return fmt.Errorf("widget %s config version %d is unsupported", section.Widget, section.ConfigVersion)
		}
		// Only presentation properties are validated here. Asset references,
		// selections, queries and actions retain their original host owners.
		values := map[string]string{"title": section.Title, "width": section.Width, "text": section.Text, "group": section.Group, "measure": section.Measure}
		props := map[string]string{}
		for key, value := range values {
			if value != "" {
				if _, declared := widget.PropsSchema.Properties[key]; !declared {
					return fmt.Errorf("widget %s does not declare property %s", section.Widget, key)
				}
				props[key] = value
			}
		}
		raw, _ := json.Marshal(props)
		return widget.PropsSchema.Validate(raw, 1<<20)
	}
	return fmt.Errorf("widget %q is unavailable", section.Widget)
}
