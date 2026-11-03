package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

const manifestPrefix = "export const pageUIManifest = "

type rulesFile struct {
	Format             json.RawMessage       `json:"format"`
	Section            json.RawMessage       `json:"section"`
	Scope              json.RawMessage       `json:"scope"`
	ScalarFieldPaths   json.RawMessage       `json:"scalarFieldPaths"`
	ArrayFieldPaths    json.RawMessage       `json:"arrayFieldPaths"`
	RepeatedFieldPaths json.RawMessage       `json:"repeatedFieldPaths"`
	ScopeRules         map[string]string     `json:"scopeRules"`
	Widgets            map[string]ruleWidget `json:"widgets"`
}

type ruleWidget struct {
	Resources json.RawMessage `json:"resources"`
}

type manifestFile struct {
	UIProfile         json.RawMessage `json:"uiProfile"`
	Widgets           []widget        `json:"widgets"`
	SupportedProfiles json.RawMessage `json:"supportedProfiles"`
	Documentless      json.RawMessage `json:"documentless"`
	Features          json.RawMessage `json:"features"`
	Runtime           json.RawMessage `json:"runtime"`
}

type widget struct {
	ComponentID         string          `json:"componentID"`
	ConfigVersion       int             `json:"configVersion"`
	RequiredUIProfile   string          `json:"requiredUIProfile"`
	SelectionMode       string          `json:"selectionMode"`
	BindingKinds        []string        `json:"bindingKinds"`
	InputPorts          []port          `json:"inputPorts"`
	OutputPorts         []port          `json:"outputPorts"`
	ConfigurationFields []string        `json:"configurationFields,omitempty"`
	Resources           json.RawMessage `json:"resources,omitempty"`
	Events              json.RawMessage `json:"events,omitempty"`
	Slots               json.RawMessage `json:"slots,omitempty"`
	FieldRoles          json.RawMessage `json:"fieldRoles,omitempty"`
	PortGroups          json.RawMessage `json:"portGroups,omitempty"`
	ModelPorts          json.RawMessage `json:"modelPorts,omitempty"`
}

type port struct {
	ID                string          `json:"id"`
	BindingField      string          `json:"bindingField"`
	Type              string          `json:"type"`
	RequiredUIProfile string          `json:"requiredUIProfile"`
	Writable          *bool           `json:"writable,omitempty"`
	StateWritable     *bool           `json:"stateWritable,omitempty"`
	Required          *bool           `json:"required,omitempty"`
	Plan              json.RawMessage `json:"plan,omitempty"`
	Modes             []string        `json:"modes,omitempty"`
	Scopes            []string        `json:"scopes,omitempty"`
	Source            string          `json:"source,omitempty"`
	SameOwner         string          `json:"sameOwner,omitempty"`
	ScopeRule         string          `json:"scopeRule,omitempty"`
}

type contract struct {
	Format             json.RawMessage  `json:"format"`
	Section            json.RawMessage  `json:"section"`
	Scope              json.RawMessage  `json:"scope"`
	ScalarFieldPaths   json.RawMessage  `json:"scalarFieldPaths"`
	RetainedContract   retainedContract `json:"retainedContract"`
	ArrayFieldPaths    json.RawMessage  `json:"arrayFieldPaths"`
	RepeatedFieldPaths json.RawMessage  `json:"repeatedFieldPaths"`
}

type retainedContract struct {
	UIProfile         json.RawMessage `json:"uiProfile"`
	Widgets           []widget        `json:"widgets"`
	SupportedProfiles json.RawMessage `json:"supportedProfiles"`
	Documentless      json.RawMessage `json:"documentless"`
	Features          json.RawMessage `json:"features"`
	Runtime           json.RawMessage `json:"runtime"`
}

func main() {
	manifestPath := flag.String("manifest", "../../web/packages/app/src/pageui/manifest.ts", "the page UI manifest")
	rulesPath := flag.String("rules", "platform/page_compat_rules.json", "the server-only page contract rules")
	compatPath := flag.String("compat", "platform/page_compat.json", "the generated server page contract")
	safetyPath := flag.String("safety", "../../web/packages/app/src/pageui/safety.ts", "the generated page safety contract")
	flag.Parse()

	manifest, err := readManifest(*manifestPath)
	if err != nil {
		fail(err)
	}
	rules, err := readRules(*rulesPath)
	if err != nil {
		fail(err)
	}

	byID := map[string]bool{}
	for i := range manifest.Widgets {
		widget := &manifest.Widgets[i]
		byID[widget.ComponentID] = true
	}
	if err := applyScopeRules(manifest.Widgets, rules.ScopeRules); err != nil {
		fail(err)
	}
	runtime, err := projectRuntime(manifest.Runtime)
	if err != nil {
		fail(err)
	}
	for id, rule := range rules.Widgets {
		if !byID[id] {
			fail(fmt.Errorf("rules widget %s is not in the manifest", id))
		}
		for i := range manifest.Widgets {
			if manifest.Widgets[i].ComponentID == id {
				manifest.Widgets[i].Resources = rule.Resources
				break
			}
		}
	}

	generated := contract{
		Format:           rules.Format,
		Section:          rules.Section,
		Scope:            rules.Scope,
		ScalarFieldPaths: rules.ScalarFieldPaths,
		RetainedContract: retainedContract{
			UIProfile:         manifest.UIProfile,
			Widgets:           manifest.Widgets,
			SupportedProfiles: manifest.SupportedProfiles,
			Documentless:      manifest.Documentless,
			Features:          manifest.Features,
			Runtime:           runtime,
		},
		ArrayFieldPaths:    rules.ArrayFieldPaths,
		RepeatedFieldPaths: rules.RepeatedFieldPaths,
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(generated); err != nil {
		fail(err)
	}
	body := bytes.TrimRight(buf.Bytes(), "\n")
	compat := append(append([]byte{}, body...), '\n')
	if err := os.WriteFile(*compatPath, compat, 0o644); err != nil {
		fail(err)
	}
	safety := append([]byte("export const pageSafetyContract = "), body...)
	safety = append(safety, []byte(" as const;\n")...)
	if err := os.WriteFile(*safetyPath, safety, 0o644); err != nil {
		fail(err)
	}
}

func projectRuntime(raw json.RawMessage) (json.RawMessage, error) {
	var source map[string]json.RawMessage
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	kept := map[string]json.RawMessage{}
	for _, name := range []string{
		"composite", "modelBinding", "computeResource", "embedding", "exploration", "telemetry",
		"contextViews", "dateInput", "recordSelection", "tableEditing", "scope", "decimal",
		"application", "overlay", "valueTypes", "maxVariables", "maxStringBytes", "operators",
		"resources", "loop", "aggregate", "query", "interface",
	} {
		value, ok := source[name]
		if !ok {
			return nil, fmt.Errorf("page runtime lacks %s", name)
		}
		kept[name] = value
	}
	return json.Marshal(kept)
}

func applyScopeRules(widgets []widget, rules map[string]string) error {
	for key, rule := range rules {
		parts := strings.Split(key, ".")
		if len(parts) != 3 || parts[1] != "inputPorts" && parts[1] != "outputPorts" {
			return fmt.Errorf("scope rule %q must be widget.ports.port", key)
		}
		found := false
		for i := range widgets {
			if widgets[i].ComponentID != parts[0] {
				continue
			}
			ports := &widgets[i].InputPorts
			if parts[1] == "outputPorts" {
				ports = &widgets[i].OutputPorts
			}
			for j := range *ports {
				if (*ports)[j].ID == parts[2] {
					(*ports)[j].ScopeRule = rule
					found = true
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("scope rule %q names no manifest port", key)
		}
	}
	return nil
}

func readManifest(path string) (manifestFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return manifestFile{}, err
	}
	text := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(text, manifestPrefix) {
		return manifestFile{}, fmt.Errorf("%s does not start with %q", path, manifestPrefix)
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, manifestPrefix), "as const;"))
	var manifest manifestFile
	if err := json.Unmarshal([]byte(body), &manifest); err != nil {
		return manifestFile{}, fmt.Errorf("%s: %w", path, err)
	}
	return manifest, nil
}

func readRules(path string) (rulesFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return rulesFile{}, err
	}
	var rules rulesFile
	if err := json.Unmarshal(raw, &rules); err != nil {
		return rulesFile{}, fmt.Errorf("%s: %w", path, err)
	}
	return rules, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
