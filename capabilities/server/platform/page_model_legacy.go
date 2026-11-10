package platform

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"
)

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
