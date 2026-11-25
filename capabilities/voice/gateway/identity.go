package gateway

import (
	"strings"

	"github.com/emiago/sipgo/sip"
)

func identity(req *sip.Request) (string, string) {
	if h := req.GetHeader("P-Asserted-Identity"); h != nil {
		if number, name := parseIdentity(h.Value()); number != "" {
			return number, name
		}
	}
	if from := req.From(); from != nil {
		return from.Address.User, from.DisplayName
	}
	return "", ""
}

func parseIdentity(value string) (string, string) {
	var number, name string
	for _, part := range strings.Split(value, ",") {
		display, uri := splitNameAddr(strings.TrimSpace(part))
		if name == "" {
			name = display
		}
		user := uriUser(uri)
		if user == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(uri), "tel:") {
			return user, name
		}
		if number == "" {
			number = user
		}
	}
	return number, name
}

func splitNameAddr(s string) (string, string) {
	open := strings.IndexByte(s, '<')
	end := strings.LastIndexByte(s, '>')
	if open < 0 || end < open {
		return "", s
	}
	return strings.Trim(strings.TrimSpace(s[:open]), `"`), s[open+1 : end]
}

func uriUser(uri string) string {
	lower := strings.ToLower(uri)
	switch {
	case strings.HasPrefix(lower, "tel:"):
		number, _, _ := strings.Cut(uri[len("tel:"):], ";")
		return number
	case strings.HasPrefix(lower, "sip:"), strings.HasPrefix(lower, "sips:"):
		_, rest, _ := strings.Cut(uri, ":")
		user, _, found := strings.Cut(rest, "@")
		if !found {
			return ""
		}
		user, _, _ = strings.Cut(user, ";")
		return user
	}
	return ""
}

func redact(number string) string {
	if len(number) <= 2 {
		return "**"
	}
	return strings.Repeat("*", len(number)-2) + number[len(number)-2:]
}
