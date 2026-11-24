package protocol

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"platformserver/kernel/values"
	"platformserver/platform"
)

const (
	AssetImplementation     platform.AssetKind = "implementation"
	ImplementationConnector                    = "connector"
)

var implementationPlaceholder = regexp.MustCompile(`\{([^{}]*)\}`)

type ProtocolImplementation struct {
	Name        string                  `json:"name"`
	Title       string                  `json:"title"`
	Description string                  `json:"description,omitempty"`
	Contract    string                  `json:"contract"`
	Kind        string                  `json:"kind"`
	Origins     []string                `json:"origins,omitempty"`
	Credential  string                  `json:"credential,omitempty"`
	Requests    []ImplementationRequest `json:"requests,omitempty"`
}

type ImplementationRequest struct {
	Action string `json:"action"`
	Method string `json:"method"`
	Path   string `json:"path"`
}

func ImplementationIdentity(app, name string) string { return app + ":" + name }

func SplitImplementationIdentity(id string) (string, string, bool) {
	app, name, found := strings.Cut(id, ":")
	return app, name, found && app != "" && name != ""
}

func ConnectorOrigin(address string) string {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(address, "?") || strings.HasSuffix(address, "#") {
		return ""
	}
	return u.Scheme + "://" + strings.ToLower(u.Host)
}

func (i ProtocolImplementation) Check() error {
	if len(i.Name) > 120 || !contractName.MatchString(i.Name) || strings.TrimSpace(i.Title) == "" || len(i.Title) > 200 || len(i.Description) > 4000 {
		return fmt.Errorf("A capability implementation needs a lowercase dotted name and a title")
	}
	name, major, found := strings.Cut(i.Contract, "/")
	number, err := strconv.Atoi(major)
	if !found || !contractName.MatchString(name) || err != nil || number < 1 || number > 999 || strconv.Itoa(number) != major {
		return fmt.Errorf("Implementation %s names its contract as name/major, such as telephony.call/1", i.Name)
	}
	if i.Kind != ImplementationConnector {
		return fmt.Errorf("Implementation %s must be a connector; other kinds come later", i.Name)
	}
	if len(i.Origins) == 0 || len(i.Origins) > 8 {
		return fmt.Errorf("Connector %s lists 1 to 8 outbound origins", i.Name)
	}
	for _, origin := range i.Origins {
		if ConnectorOrigin(origin) != origin {
			return fmt.Errorf("Connector %s: outbound origin %q is not an exact https origin such as https://api.example.com", i.Name, origin)
		}
	}
	if i.Credential != "" && i.Credential != "api-key" && i.Credential != "bearer" {
		return fmt.Errorf("Connector %s asks for an api-key or a bearer credential", i.Name)
	}
	if len(i.Requests) == 0 || len(i.Requests) > 64 {
		return fmt.Errorf("Connector %s maps 1 to 64 contract actions to requests", i.Name)
	}
	seen := map[string]bool{}
	for _, r := range i.Requests {
		if seen[r.Action] || !values.Identifier.MatchString(r.Action) || !slices.Contains([]string{"GET", "POST", "PUT", "PATCH", "DELETE"}, r.Method) ||
			!strings.HasPrefix(r.Path, "/") || strings.HasPrefix(r.Path, "//") || len(r.Path) > 500 || strings.ContainsAny(r.Path, " \t\n#\\") || strings.Contains(r.Path, "://") {
			return fmt.Errorf("Connector %s: each contract action has one request with GET, POST, PUT, PATCH or DELETE and a path starting with /", i.Name)
		}
		seen[r.Action] = true
	}
	return nil
}

func CheckConnector(c ProtocolContract, i ProtocolImplementation) error {
	if c.ID() != i.Contract {
		return fmt.Errorf("Connector %s implements %s, not %s", i.Name, i.Contract, c.ID())
	}
	if len(c.Reads) > 0 {
		return fmt.Errorf("Connector %s cannot serve the reads of %s; a connector implements actions only", i.Name, c.ID())
	}
	for _, action := range c.Actions {
		at := slices.IndexFunc(i.Requests, func(r ImplementationRequest) bool { return r.Action == action.Name })
		if at < 0 {
			return fmt.Errorf("Connector %s has no request for action %s of %s", i.Name, action.Name, c.ID())
		}
		for _, match := range implementationPlaceholder.FindAllStringSubmatch(i.Requests[at].Path, -1) {
			if !slices.ContainsFunc(action.Payload, func(f platform.Field) bool { return f.Name == match[1] }) {
				return fmt.Errorf("Connector %s: the path of %s names {%s}, which is not a parameter of action %s", i.Name, action.Name, match[1], action.Name)
			}
		}
	}
	for _, r := range i.Requests {
		if !slices.ContainsFunc(c.Actions, func(a ContractAction) bool { return a.Name == r.Action }) {
			return fmt.Errorf("Connector %s maps %s, which is not an action of %s", i.Name, r.Action, c.ID())
		}
	}
	return nil
}
