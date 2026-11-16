package pages

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"platformserver/platform"
)

var externalFrameASCII = regexp.MustCompile(`^[!-~]+$`)
var externalFrameOrigin = regexp.MustCompile(`^https://[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[1-9][0-9]{0,4})?$`)

func validPageImageURL(raw string) bool {
	if raw == "" {
		return true
	}
	if len(raw) > platform.PageWidgets.Runtime.ContextViews.MaxURLBytes || !utf8.ValidString(raw) {
		return false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) {
		return false
	}
	for _, text := range []string{raw, decoded} {
		for _, r := range text {
			if r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
				return false
			}
		}
	}
	if strings.HasPrefix(raw, "/") {
		return !strings.HasPrefix(raw, "//") && !strings.HasPrefix(decoded, "//")
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || u.Hostname() == "" {
		return false
	}
	if strings.Contains(u.Host, "[") && net.ParseIP(u.Hostname()) == nil {
		return false
	}
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > 65535 {
			return false
		}
	}
	return true
}

func validPageExternalFrame(c platform.PageExternalFrame) bool {
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
