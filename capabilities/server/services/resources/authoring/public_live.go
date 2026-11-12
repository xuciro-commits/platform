package build

import (
	"encoding/json"
	"net/url"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func PublicLiveKey(m platform.Member, query, version, language string, entity func(string) (platform.EntityInfo, bool)) (string, bool) {
	target, body, post := strings.Cut(query, "\n")
	if post || body != "" {
		return "", false
	}
	u, err := url.Parse(target)
	if err != nil || !strings.HasPrefix(u.Path, "/v1/records/") {
		return "", false
	}
	typ, tail, _ := strings.Cut(strings.TrimPrefix(u.Path, "/v1/records/"), "/")
	if typ == "" || tail != "" {
		return "", false
	}
	for name := range u.Query() {
		if !slices.Contains([]string{"limit", "offset", "sort", "archived"}, name) {
			return "", false
		}
	}
	info, found := entity(typ)
	if !found || info.Scope.Participants != nil || info.Scope.Through != nil || ProjectDelegated(m) {
		return "", false
	}
	roles := slices.Clone(m.RolesIn(info.App))
	for _, role := range roles {
		if info.Scope.Level(role) != platform.ScopeTenant {
			return "", false
		}
	}
	slices.Sort(roles)
	profile, _ := json.Marshal([]any{roles, slices.Sorted(slices.Values(m.Markings)), slices.Sorted(slices.Values(m.Declassifications)), slices.Sorted(slices.Values(m.Scopes)), m.AIContext})
	return strings.Join([]string{query, version, time.Now().UTC().Format(time.DateOnly), language, string(profile)}, "\n"), true
}
