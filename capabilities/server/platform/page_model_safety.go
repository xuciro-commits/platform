package platform

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

func (p Page) Safety() (Page, error) {
	if p.Format != 4 {
		return Page{}, fmt.Errorf("unsupported page format %d", p.Format)
	}
	if p.RetainedVersion == "" {
		version, err := PageContentVersion(p)
		if err != nil {
			return Page{}, err
		}
		p.RetainedVersion = version
	}
	return p, nil
}

func (p Page) CheckSafety() error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, binding := range safe.DataBindings {
		if !pageNodeID.MatchString(binding.ID) || ids[binding.ID] {
			return fmt.Errorf("page data bindings need unique stable IDs")
		}
		ids[binding.ID] = true
	}
	scopes := map[string]bool{}
	for _, scope := range safe.ScopeVisibility.Scopes {
		if !slices.Contains([]string{"page", "application", "overlay", "loop"}, scope.Kind) {
			return fmt.Errorf("unknown page scope kind")
		}
		if !pageNodeID.MatchString(scope.ID) || scopes[scope.ID] {
			return fmt.Errorf("page scopes need unique stable IDs")
		}
		scopes[scope.ID] = true
		if scope.Binding != "" && !ids[scope.Binding] {
			return fmt.Errorf("page scope binding is unavailable")
		}
	}
	for _, scope := range safe.ScopeVisibility.Scopes {
		if scope.Parent != "" && !scopes[scope.Parent] {
			return fmt.Errorf("page scope parent is unavailable")
		}
		if scope.ID == safe.ScopeVisibility.Root && scope.Parent != "" {
			return fmt.Errorf("the page root scope has no parent")
		}
	}
	for _, scope := range safe.ScopeVisibility.Scopes {
		seen := map[string]bool{}
		owner := scope.ID
		for owner != "" {
			if seen[owner] {
				return fmt.Errorf("page scope graph contains a cycle")
			}
			seen[owner] = true
			next := ""
			for _, parent := range safe.ScopeVisibility.Scopes {
				if parent.ID == owner {
					next = parent.Parent
					break
				}
			}
			owner = next
		}
		if scope.Kind == "loop" && (scope.MaxIterations < 1 || scope.MaxIterations > pageWidgets.Runtime.Loop.MaxItems || safe.InputSchema.Variables[scope.Bindings["collection"]].Type != "object-set" || safe.InputSchema.Variables[scope.Bindings["itemVariable"]].Type != "record") {
			return fmt.Errorf("page loop needs its bounded typed collection and item")
		}
	}
	for _, binding := range safe.DataBindings {
		owner := safe.ScopeVisibility.Owners[binding.ID]
		if owner == "" {
			owner = binding.Owner
		}
		for _, id := range pageBindingVariables(binding) {
			if id == "" {
				continue
			}
			variable, ok := safe.InputSchema.Variables[id]
			if !ok {
				return fmt.Errorf("page binding variable %s is unavailable", id)
			}
			if variable.Scope == "overlay" && safe.scopeOwner(owner, "overlay") != variable.Owner || variable.Scope == "loop-item" && safe.scopeOwner(owner, "loop") != variable.Owner {
				return fmt.Errorf("page binding variable escapes its original owner")
			}
		}
		if err := safe.checkWidgetPorts(binding, owner); err != nil {
			return err
		}
	}
	triggers := map[string]bool{}
	for _, trigger := range safe.ActionTriggers {
		if trigger.ID == "" || triggers[trigger.ID] || trigger.Binding != "page" && !ids[trigger.Binding] && !scopes[trigger.Binding] {
			return fmt.Errorf("page trigger needs a unique ID and original owner")
		}
		triggers[trigger.ID] = true
	}
	if err := safe.checkEnvelope(); err != nil {
		return err
	}
	if err := safe.InputSchema.checkProfile(); err != nil {
		return err
	}
	if err := safe.InputSchema.CheckVariables(); err != nil {
		return err
	}
	if err := safe.checkQueries("page"); err != nil {
		return err
	}
	if err := safe.InputSchema.checkInterface(); err != nil {
		return err
	}
	if safe.Object.Name != "" {
		if err := safe.CheckRecordPorts(); err != nil {
			return err
		}
	}
	return safe.checkEvents()
}

func (p *Page) UnmarshalJSON(raw []byte) error {
	type value Page
	var decoded value
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*p = Page(decoded)
	if p.Format != 4 {
		return fmt.Errorf("unsupported page format %d", p.Format)
	}
	return p.checkEnvelope()
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
