package platform

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

func legacyPagePathValues(value any, path []string) []any {
	if len(path) == 0 {
		return []any{value}
	}
	switch current := value.(type) {
	case map[string]any:
		key := strings.TrimSuffix(path[0], "[]")
		return legacyPagePathValues(current[key], path[1:])
	case []any:
		result := []any{}
		for _, child := range current {
			result = append(result, legacyPagePathValues(child, path)...)
		}
		return result
	}
	return nil
}

func (p Page) MarshalJSON() ([]byte, error) {
	type legacyPage Page
	if p.Format != 3 && p.Format != 4 {
		return json.Marshal(legacyPage(p))
	}
	type envelope struct {
		Format            int                 `json:"format"`
		Name              string              `json:"name"`
		Title             string              `json:"title"`
		Description       string              `json:"description,omitempty"`
		Object            AssetRef            `json:"object"`
		DataBindings      []PageDataBinding   `json:"dataBindings"`
		ActionTriggers    []PageActionTrigger `json:"actionTriggers"`
		InputSchema       PageInputSchema     `json:"inputSchema"`
		ScopeVisibility   PageScopeVisibility `json:"scopeVisibility"`
		WidgetSpec        PageWidgetSpec      `json:"widgetSpec"`
		PresentationWords map[string]string   `json:"presentationWords,omitempty"`
	}
	return json.Marshal(envelope{p.Format, p.Name, p.Title, p.Description, p.Object, p.DataBindings, p.ActionTriggers, p.InputSchema, p.ScopeVisibility, p.WidgetSpec, p.PresentationWords})
}

func (p *Page) UnmarshalJSON(raw []byte) error {
	type legacyPage Page
	var decoded legacyPage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*p = Page(decoded)
	if p.Format != 0 && p.Format != 3 && p.Format != 4 {
		return fmt.Errorf("unsupported page format %d", p.Format)
	}
	if p.Format == 3 || p.Format == 4 {
		return p.checkEnvelope()
	}
	return nil
}

func RetainPageSection(raw []byte, target any, visual *map[string]json.RawMessage) error {
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	values := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}
	known := map[string]bool{}
	var fields func(reflect.Type)
	fields = func(typ reflect.Type) {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Anonymous {
				fields(field.Type)
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				known[name] = true
			}
		}
	}
	typ := reflect.TypeOf(target)
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	fields(typ)
	for name := range known {
		delete(values, name)
	}
	*visual = values
	return nil
}

func MarshalPageSection(value any, visual map[string]json.RawMessage) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	values := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	for name, raw := range visual {
		if len(raw) > 0 {
			if _, known := values[name]; known {
				continue
			}
			values[name] = raw
		}
	}
	return json.Marshal(values)
}

func (s *Section) UnmarshalJSON(raw []byte) error {
	type value Section
	return RetainPageSection(raw, (*value)(s), &s.LegacyVisual)
}

func (s Section) MarshalJSON() ([]byte, error) {
	type value Section
	return MarshalPageSection(value(s), s.LegacyVisual)
}

var pageCompatibility = func() struct {
	Section struct {
		BindingPaths  []string `json:"bindingPaths"`
		ResourcePaths []string `json:"resourcePaths"`
	} `json:"section"`
	Scope struct {
		BindingPaths  []string `json:"bindingPaths"`
		OverlayPaths  []string `json:"overlayPaths"`
		DocumentPaths []string `json:"documentPaths"`
	} `json:"scope"`
	ScalarFieldPaths []string `json:"scalarFieldPaths"`
	ArrayFieldPaths  []string `json:"arrayFieldPaths"`
} {
	var result struct {
		Section struct {
			BindingPaths  []string `json:"bindingPaths"`
			ResourcePaths []string `json:"resourcePaths"`
		} `json:"section"`
		Scope struct {
			BindingPaths  []string `json:"bindingPaths"`
			OverlayPaths  []string `json:"overlayPaths"`
			DocumentPaths []string `json:"documentPaths"`
		} `json:"scope"`
		ScalarFieldPaths []string `json:"scalarFieldPaths"`
		ArrayFieldPaths  []string `json:"arrayFieldPaths"`
	}
	if err := json.Unmarshal(pageWidgetJSON, &result); err != nil {
		panic(err)
	}
	return result
}()

func stripLegacyPagePath(value any, path []string) {
	if len(path) == 0 {
		return
	}
	switch current := value.(type) {
	case map[string]any:
		key := strings.TrimSuffix(path[0], "[]")
		if len(path) == 1 {
			delete(current, key)
			return
		}
		stripLegacyPagePath(current[key], path[1:])
	case []any:
		for _, child := range current {
			stripLegacyPagePath(child, path)
		}
	}
}

func legacyPageResources(raw []byte, widget string) ([]PageResourceBinding, error) {
	var old struct {
		Scene *struct {
			Mappings []struct{ ID, Source, Field string }
		}
		AI          *struct{ Kind, ReplyField, QuestionVariable string }
		RecordLinks []struct {
			Object AssetRef
			Field  string
		}
		Embedding *struct {
			ReadOnly         bool
			Kind             string
			Page             AssetBinding
			ContentVersion   string
			InterfaceVersion int
			Inputs           map[string]PageValue
			Results          map[string]string
		}
		ExternalFrame  *struct{ URL, Origin string }
		Image          *struct{ URL string }
		AssetDirectory *struct {
			Items []struct {
				ID    string
				Asset AssetBinding
			}
		}
		GraphExplorer *struct {
			Objects []struct {
				Object     AssetRef
				LabelField string
			}
			Relations []struct {
				ID      string
				Binding AssetBinding
			}
			Outputs []struct {
				ID       string
				Object   AssetRef
				Variable string
			}
		}
		VertexGraph *struct {
			Groups []struct {
				ID        string
				Binding   AssetBinding
				Direction string
				Limit     int
			}
		}
		Observation *struct {
			Kind, TimeField, AssetField, AverageField, RowOutput, AssetOutput string
			AssetObject                                                       *AssetRef
			Signals                                                           []struct{ Field string }
			Metadata                                                          *struct{ Plant, Device, Asset, Status string }
		}
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		return nil, err
	}
	resources := []PageResourceBinding{}
	if old.Scene != nil {
		for _, m := range old.Scene.Mappings {
			resources = append(resources, PageResourceBinding{ID: m.ID, Kind: "record-mapping", Source: m.Source, Fields: []FieldRef{{Name: m.Field}}})
		}
	}
	if r := old.AI; r != nil {
		v := PageResourceBinding{ID: "ai", Kind: "ai", Source: r.Kind, Outputs: map[string]string{"question": r.QuestionVariable}}
		if r.ReplyField != "" {
			v.Fields = []FieldRef{{Name: r.ReplyField, Slot: "replyField"}}
		}
		resources = append(resources, v)
	}
	for _, r := range old.RecordLinks {
		object := r.Object
		resources = append(resources, PageResourceBinding{ID: r.Object.String() + "/" + r.Field, Kind: "incoming-reference", Object: &object, Fields: []FieldRef{{Name: r.Field}}})
	}
	if r := old.Embedding; r != nil {
		resources = append(resources, PageResourceBinding{ReadOnly: r.ReadOnly, ID: "embedding", Kind: "page", Asset: &r.Page, ContentVersion: r.ContentVersion, InterfaceVersion: r.InterfaceVersion, Inputs: r.Inputs, Outputs: r.Results, Source: r.Kind})
	}
	if r := old.ExternalFrame; r != nil {
		resources = append(resources, PageResourceBinding{ID: "externalFrame", Kind: "external-document", URL: r.URL, Origin: r.Origin})
	}
	if r := old.Image; r != nil {
		resources = append(resources, PageResourceBinding{ID: "image", Kind: "image", URL: r.URL})
	}
	if r := old.AssetDirectory; r != nil {
		for _, item := range r.Items {
			binding := item.Asset
			resources = append(resources, PageResourceBinding{ID: item.ID, Kind: "asset", Asset: &binding})
		}
	}
	if r := old.GraphExplorer; r != nil {
		for _, item := range r.Objects {
			object := item.Object
			resources = append(resources, PageResourceBinding{ID: object.String(), Kind: "graph-object", Object: &object, Fields: []FieldRef{{Name: item.LabelField}}})
		}
		for _, item := range r.Relations {
			binding := item.Binding
			resources = append(resources, PageResourceBinding{ID: item.ID, Kind: "graph-link", Asset: &binding})
		}
		for _, item := range r.Outputs {
			object := item.Object
			resources = append(resources, PageResourceBinding{ID: item.ID, Kind: "record-output", Object: &object, Outputs: map[string]string{"record": item.Variable}})
		}
	}
	if r := old.VertexGraph; r != nil {
		for _, item := range r.Groups {
			binding := item.Binding
			resources = append(resources, PageResourceBinding{ID: item.ID, Kind: "neighborhood", Asset: &binding, Source: item.Direction, Limit: item.Limit})
		}
	}
	if r := old.Observation; r != nil {
		v := PageResourceBinding{ID: "observation", Kind: "observation", Object: r.AssetObject, Source: r.Kind, Outputs: map[string]string{"row": r.RowOutput, "asset": r.AssetOutput}}
		for slot, name := range map[string]string{"timeField": r.TimeField, "assetField": r.AssetField, "averageField": r.AverageField} {
			if name != "" {
				v.Fields = append(v.Fields, FieldRef{Name: name, Slot: slot})
			}
		}
		if r.Metadata != nil {
			for slot, name := range map[string]string{"metadata.plant": r.Metadata.Plant, "metadata.device": r.Metadata.Device, "metadata.asset": r.Metadata.Asset, "metadata.status": r.Metadata.Status} {
				if name != "" {
					v.Fields = append(v.Fields, FieldRef{Name: name, Slot: slot})
				}
			}
		}
		for _, signal := range r.Signals {
			v.Fields = append(v.Fields, FieldRef{Name: signal.Field, Slot: "signals"})
		}
		resources = append(resources, v)
	}
	if frozen := pageWidget(widget); frozen != nil {
		resources = append(resources, frozen.Resources...)
	}
	return resources, nil
}

var externalFrameASCII = regexp.MustCompile(`^[!-~]+$`)

var externalFrameOrigin = regexp.MustCompile(`^https://[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[1-9][0-9]{0,4})?$`)

func ValidPageExternalFrame(c PageExternalFrame) bool {
	if len(c.URL) > 2048 || !externalFrameASCII.MatchString(c.URL) || !externalFrameOrigin.MatchString(c.Origin) || strings.ContainsAny(c.URL, "\\ \t\r\n") || !strings.HasPrefix(c.URL, c.Origin+"/") {
		return false
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || "https://"+u.Host != c.Origin || u.Port() == "443" || u.String() != c.URL {
		return false
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	if u.Port() != "" {
		var port int
		if _, err := fmt.Sscan(u.Port(), &port); err != nil || port > 65535 {
			return false
		}
	}
	return true
}

func (p *PageExternalFrame) UnmarshalJSON(raw []byte) error {
	type value PageExternalFrame
	return RetainPageSection(raw, (*value)(p), &p.LegacyVisual)
}

func (p PageExternalFrame) MarshalJSON() ([]byte, error) {
	type value PageExternalFrame
	return MarshalPageSection(value(p), p.LegacyVisual)
}

func checkLegacyPageProfile(document *PageDocument, section Section, raw []byte) error {
	contract := pageWidget(section.Widget)
	if contract == nil {
		return fmt.Errorf("unsupported retained page component %s", section.Widget)
	}
	if section.ConfigVersion != 0 && section.ConfigVersion != contract.ConfigVersion {
		return fmt.Errorf("unsupported retained component version")
	}
	if document == nil {
		if !slices.Contains(pageWidgets.Documentless.Widgets, section.Widget) {
			return fmt.Errorf("retained component needs a page document")
		}
		return nil
	}
	if !PageUIProfileSupports(document.UIProfile, contract.RequiredUIProfile) {
		return fmt.Errorf("retained component is newer than its original profile")
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}
	for _, port := range append(slices.Clone(contract.InputPorts), contract.OutputPorts...) {
		for _, value := range legacyPagePathValues(values, strings.Split(port.BindingField, ".")) {
			if text, ok := value.(string); ok && text != "" && port.RequiredUIProfile != "" && !PageUIProfileSupports(document.UIProfile, port.RequiredUIProfile) {
				return fmt.Errorf("retained binding is newer than its original profile")
			}
		}
	}
	return nil
}

func legacyFieldRef(name string) FieldRef {
	if strings.Contains(name, ".") {
		return FieldRef{Path: strings.Split(name, ".")}
	}
	return FieldRef{Name: name}
}
