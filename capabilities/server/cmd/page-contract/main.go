package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	resourceauthoring "platformserver/services/resources/authoring"
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

type applicationRegistry struct {
	Format    int                     `json:"format"`
	Manifests []registeredManifest    `json:"manifests"`
	Apps      []registeredApplication `json:"applications"`
}

type registeredManifest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type registeredAccess struct {
	App      string   `json:"app,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	Schema   string   `json:"schema,omitempty"`
	Entities bool     `json:"entities,omitempty"`
	All      bool     `json:"all,omitempty"`
}

type registeredRoute struct {
	View string `json:"view"`
}

type registeredHome struct {
	Route registeredRoute    `json:"route"`
	When  []registeredAccess `json:"when,omitempty"`
	Shell bool               `json:"shell,omitempty"`
}

type registeredOpen struct {
	Type string             `json:"type"`
	View string             `json:"view"`
	When []registeredAccess `json:"when,omitempty"`
}

type registeredApplication struct {
	ID       string             `json:"id"`
	Title    string             `json:"title"`
	Manifest string             `json:"manifest,omitempty"`
	Serves   []string           `json:"serves"`
	Surface  string             `json:"surface,omitempty"`
	Category string             `json:"category"`
	Portal   bool               `json:"portal"`
	Access   []registeredAccess `json:"access"`
	Home     []registeredHome   `json:"home"`
	Opens    []registeredOpen   `json:"opens"`
}

type applicationContract struct {
	Manifests map[string]registeredManifest    `json:"manifests"`
	Apps      map[string]registeredApplication `json:"applications"`
}

type resourceContract struct {
	Format    int                                       `json:"format"`
	Resources []resourceauthoring.ResourceMetadataEntry `json:"resources"`
}

func main() {
	manifestPath := flag.String("manifest", "../../web/packages/app/src/pageui/manifest.ts", "the page UI manifest")
	rulesPath := flag.String("rules", "platform/page_compat_rules.json", "the server-only page contract rules")
	compatPath := flag.String("compat", "platform/page_compat.json", "the generated server page contract")
	safetyPath := flag.String("safety", "../../web/packages/app/src/pageui/safety.ts", "the generated page safety contract")
	appRegistryPath := flag.String("app-registry", "platform/app_registry.json", "the application registry")
	appContractPath := flag.String("app-contract", "../../web/packages/app/src/gen/apps.ts", "the generated frontend application registry")
	resourceContractPath := flag.String("resource-contract", "../../web/packages/build/src/gen/resources.ts", "the generated frontend resource contract")
	flag.Parse()

	resources, err := resourceauthoring.ResourceMetadata()
	if err != nil {
		fail(err)
	}
	if err := writeResourceContract(*resourceContractPath, resources); err != nil {
		fail(err)
	}

	appRegistry, err := readApplicationRegistry(*appRegistryPath, resources)
	if err != nil {
		fail(err)
	}
	if err := writeApplicationContract(*appContractPath, appRegistry); err != nil {
		fail(err)
	}

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

func readApplicationRegistry(path string, resources []resourceauthoring.ResourceMetadataEntry) (applicationContract, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return applicationContract{}, err
	}
	var source applicationRegistry
	if err := json.Unmarshal(raw, &source); err != nil {
		return applicationContract{}, fmt.Errorf("%s: %w", path, err)
	}
	if source.Format != 1 {
		return applicationContract{}, fmt.Errorf("%s: unsupported format %d", path, source.Format)
	}
	out := applicationContract{Manifests: map[string]registeredManifest{}, Apps: map[string]registeredApplication{}}
	for _, manifest := range source.Manifests {
		if manifest.ID == "" || manifest.Title == "" {
			return applicationContract{}, fmt.Errorf("%s: every manifest needs an ID and title", path)
		}
		if _, exists := out.Manifests[manifest.ID]; exists {
			return applicationContract{}, fmt.Errorf("%s: manifest %q is repeated", path, manifest.ID)
		}
		out.Manifests[manifest.ID] = manifest
	}
	primaryManifests, appTitles, openTypes := map[string]string{}, map[string]string{}, map[string]string{}
	for _, app := range source.Apps {
		if app.ID == "" || app.Category == "" || len(app.Access) == 0 || len(app.Home) == 0 {
			return applicationContract{}, fmt.Errorf("%s: application %q needs an ID, category, access and home", path, app.ID)
		}
		if _, exists := out.Apps[app.ID]; exists {
			return applicationContract{}, fmt.Errorf("%s: application %q is repeated", path, app.ID)
		}
		if app.Manifest != "" {
			manifest, exists := out.Manifests[app.Manifest]
			if !exists {
				return applicationContract{}, fmt.Errorf("%s: application %q names unknown manifest %q", path, app.ID, app.Manifest)
			}
			if app.Title == "" {
				app.Title = manifest.Title
			} else if app.Title != manifest.Title {
				return applicationContract{}, fmt.Errorf("%s: application %q title %q differs from manifest %q title %q", path, app.ID, app.Title, app.Manifest, manifest.Title)
			}
			if other, exists := primaryManifests[app.Manifest]; exists {
				return applicationContract{}, fmt.Errorf("%s: manifest %q is the primary manifest of both %q and %q", path, app.Manifest, other, app.ID)
			}
			primaryManifests[app.Manifest] = app.ID
		} else if app.Title == "" {
			return applicationContract{}, fmt.Errorf("%s: application %q needs a title when it has no primary manifest", path, app.ID)
		}
		if other, exists := appTitles[app.Title]; exists {
			return applicationContract{}, fmt.Errorf("%s: applications %q and %q share title %q", path, other, app.ID, app.Title)
		}
		appTitles[app.Title] = app.ID
		for _, serve := range app.Serves {
			if serve == "" {
				return applicationContract{}, fmt.Errorf("%s: application %q has an empty served owner", path, app.ID)
			}
		}
		for _, rule := range app.Access {
			if err := validateApplicationAccess(path, app.ID, rule, out.Manifests); err != nil {
				return applicationContract{}, err
			}
		}
		if app.Home[len(app.Home)-1].When != nil {
			return applicationContract{}, fmt.Errorf("%s: application %q needs a home without a condition", path, app.ID)
		}
		for _, home := range app.Home {
			if home.Route.View == "" {
				return applicationContract{}, fmt.Errorf("%s: application %q has a home without a view", path, app.ID)
			}
			for _, rule := range home.When {
				if err := validateApplicationAccess(path, app.ID, rule, out.Manifests); err != nil {
					return applicationContract{}, err
				}
			}
		}
		for _, open := range app.Opens {
			if open.Type == "" || open.View == "" {
				return applicationContract{}, fmt.Errorf("%s: application %q has an open without a resource type or view", path, app.ID)
			}
			if err := validateResourceOpen(path, app.ID, open, resources); err != nil {
				return applicationContract{}, err
			}
			owner, _, found := strings.Cut(open.Type, ".")
			if !found || !slices.Contains(app.Serves, owner) {
				return applicationContract{}, fmt.Errorf("%s: application %q opens %q but does not serve owner %q", path, app.ID, open.Type, owner)
			}
			if other, exists := openTypes[open.Type]; exists {
				return applicationContract{}, fmt.Errorf("%s: resource type %q is opened by both %q and %q", path, open.Type, other, app.ID)
			}
			openTypes[open.Type] = app.ID
			for _, rule := range open.When {
				if err := validateApplicationAccess(path, app.ID, rule, out.Manifests); err != nil {
					return applicationContract{}, err
				}
			}
		}
		out.Apps[app.ID] = app
	}
	return out, nil
}

func validateResourceOpen(path, app string, open registeredOpen, resources []resourceauthoring.ResourceMetadataEntry) error {
	owner, _, found := strings.Cut(open.Type, ".")
	if !found || !slices.ContainsFunc(resources, func(resource resourceauthoring.ResourceMetadataEntry) bool {
		return resource.App == owner
	}) {
		return nil
	}
	typ, _, _ := strings.Cut(open.Type, "/")
	if slices.ContainsFunc(resources, func(resource resourceauthoring.ResourceMetadataEntry) bool {
		return resource.Type == typ && resource.View == open.View
	}) {
		return nil
	}
	return fmt.Errorf("%s: application %q opens %q in view %q, but the resource contract has no such route", path, app, open.Type, open.View)
}

func validateApplicationAccess(path, app string, rule registeredAccess, manifests map[string]registeredManifest) error {
	declared := 0
	if rule.App != "" {
		declared++
	}
	if rule.Schema != "" {
		declared++
	}
	if rule.Entities {
		declared++
	}
	if rule.All {
		declared++
	}
	if declared != 1 {
		return fmt.Errorf("%s: application %q access rule needs exactly one of app, schema, entities or all", path, app)
	}
	for _, role := range rule.Roles {
		if role == "" {
			return fmt.Errorf("%s: application %q access rule has an empty role", path, app)
		}
	}
	if rule.Roles != nil && rule.App == "" {
		return fmt.Errorf("%s: application %q restricts roles without naming an app", path, app)
	}
	if rule.App != "" {
		if _, exists := manifests[rule.App]; !exists {
			return fmt.Errorf("%s: application %q names unknown access owner %q", path, app, rule.App)
		}
	}
	return nil
}

func writeApplicationContract(path string, registry applicationContract) error {
	manifests, err := applicationContractValue(registry.Manifests)
	if err != nil {
		return err
	}
	applications, err := applicationContractValue(registry.Apps)
	if err != nil {
		return err
	}
	body := "export const registeredManifests = " + manifests + " as const;\n" +
		"export const registeredApplications = " + applications + " as const;\n" +
		"export type RegisteredManifestID = keyof typeof registeredManifests;\n" +
		"export type RegisteredApplicationID = keyof typeof registeredApplications;\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func writeResourceContract(path string, resources []resourceauthoring.ResourceMetadataEntry) error {
	body, err := applicationContractValue(resourceContract{Format: 1, Resources: resources})
	if err != nil {
		return err
	}
	generated := "export const resourceContract = " + body + " as const;\n" +
		"export const resourcePresentations = resourceContract.resources;\n" +
		"export const draftResourcePresentations = resourcePresentations.filter(resource => resource.listed);\n" +
		"export const releaseResourcePresentations = resourcePresentations.filter(resource => resource.release);\n" +
		"export type ResourceKind = (typeof resourcePresentations)[number][\"kind\"] | \"definition\";\n" +
		"export type ReleaseKind = Extract<(typeof resourcePresentations)[number], { release: true }>[\"releaseKind\"];\n" +
		"export type ResourceRouteMode = (typeof resourcePresentations)[number][\"route\"];\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(generated), 0o644)
}

func applicationContractValue(value any) (string, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimRight(out.String(), "\n"), nil
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
