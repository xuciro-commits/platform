package platform

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// External documents carry no platform inputs, grants or message bridge.
type PageExternalFrame struct {
	URL    string `json:"url"`
	Origin string `json:"origin"`
	Height *int   `json:"height,omitempty"`
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
	return c.Height == nil || *c.Height >= 1 && *c.Height <= pageWidgets.Layout.MaxSize
}

func (d *PageDocument) checkExternalFrame(s Section) error {
	if s.Widget != "external-frame" {
		if s.ExternalFrame != nil {
			return fmt.Errorf("external frame needs its widget")
		}
		return nil
	}
	if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.84") || s.ExternalFrame == nil || !ValidPageExternalFrame(*s.ExternalFrame) || s.Embedding != nil || s.Object != (AssetRef{}) || s.RecordVariable != "" || s.CollectionVariable != "" || s.Query != (AssetRef{}) || s.Operation != nil || s.Function != nil || len(s.Fields) > 0 || len(s.Actions) > 0 || len(s.Inputs) > 0 || s.Selection != "" {
		return fmt.Errorf("external frame needs a fixed HTTPS origin without platform bindings")
	}
	return nil
}
