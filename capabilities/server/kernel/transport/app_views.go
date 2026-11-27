package transport

import (
	"cmp"
	"encoding/json"
	"maps"
	"platformserver/kernel/protocol"
	"slices"
	"strings"

	"platformserver/platform"
)

type AppEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Role  string `json:"role"`
}

type AppInfo struct {
	ID           string                    `json:"id"`
	Title        string                    `json:"title"`
	Version      string                    `json:"version"`
	Reads        []string                  `json:"reads"`
	Roles        []string                  `json:"roles"`
	Capabilities []platform.CapabilityInfo `json:"capabilities"`
	Inputs       []string                  `json:"inputs"`
	Uses         []string                  `json:"uses"`
	Subscribes   []string                  `json:"subscribes"`
	Provides     []string                  `json:"provides"`
	Consumes     []string                  `json:"consumes"`
	Emits        []platform.EffectKind     `json:"emits"`
	Interfaces   []platform.Interface      `json:"interfaces"`
}

func SpaceTitle(space string, titles map[string]string) string {
	if title := titles[space]; title != "" {
		return title
	}
	if space == "" {
		return ""
	}
	return strings.ToUpper(space[:1]) + space[1:]
}

func AppEntries(manifests []platform.Manifest, roles map[string]string, declared ...map[string]string) []AppEntry {
	var titles map[string]string
	if len(declared) > 0 {
		titles = declared[0]
	}
	out := []AppEntry{}
	seen := map[string]bool{}
	for _, m := range manifests {
		seen[m.ID] = true
		if role := roles[m.ID]; role != "" {
			out = append(out, AppEntry{ID: m.ID, Title: cmp.Or(m.Title, m.ID), Role: role})
		}
	}
	for _, app := range slices.Sorted(maps.Keys(roles)) {
		if !seen[app] && roles[app] != "" {
			out = append(out, AppEntry{ID: app, Title: SpaceTitle(app, titles), Role: roles[app]})
		}
	}
	return out
}

func AppInfos(manifests []platform.Manifest, declared ...map[string]string) []AppInfo {
	var titles map[string]string
	if len(declared) > 0 {
		titles = declared[0]
	}
	out := []AppInfo{}
	seen := map[string]bool{}
	spaces := map[string][]string{}
	for _, m := range manifests {
		seen[m.ID] = true
		info := AppInfo{ID: m.ID, Title: m.Title, Version: m.Version, Reads: append([]string{}, m.Reads...), Provides: []string{}, Consumes: []string{},
			Roles: append([]string{}, m.AllRoles()...), Capabilities: append([]platform.CapabilityInfo{}, m.Actions.Capabilities()...), Inputs: []string{}, Uses: []string{}, Subscribes: append([]string{}, m.Subscribes...), Emits: append([]platform.EffectKind{}, m.Emits...), Interfaces: append([]platform.Interface{}, m.Interfaces...)}
		for input, journaled := range m.Inputs {
			info.Inputs = append(info.Inputs, input+map[bool]string{true: "", false: " (not journaled)"}[journaled])
		}
		slices.Sort(info.Inputs)
		for _, pv := range m.Provides {
			info.Provides = append(info.Provides, pv.Protocol.ID())
		}
		for _, c := range m.Consumes {
			info.Consumes = append(info.Consumes, c.Protocol+map[bool]string{true: " (optional)", false: ""}[c.Optional])
		}
		for _, action := range m.Actions.All() {
			for _, used := range action.Uses {
				info.Uses = append(info.Uses, action.Schema+" → "+used)
			}
			if space, _, ok := strings.Cut(action.Target, "."); ok && space != "" && space != m.ID {
				for _, role := range action.Roles {
					if role != "builder" && role != platform.AnyMember && !slices.Contains(spaces[space], role) {
						spaces[space] = append(spaces[space], role)
					}
				}
			}
		}
		for _, entity := range m.Entities {
			space, _, ok := strings.Cut(entity.Type, ".")
			if !ok || space == "" || space == m.ID {
				continue
			}
			for role := range entity.Scope.Levels {
				if role != "builder" && !slices.Contains(spaces[space], role) {
					spaces[space] = append(spaces[space], role)
				}
			}
		}
		for _, role := range m.Actions.Roles() {
			if !m.Actions.Custom(role) {
				continue
			}
			spaceSet := map[string]bool{}
			ownApp := false
			for _, act := range m.Actions.ForRoles([]string{role}) {
				if slices.Contains(act.Roles, platform.AnyMember) {
					continue
				}
				if space, _, ok := strings.Cut(act.Target, "."); ok && space != "" && space != m.ID {
					spaceSet[space] = true
				} else {
					ownApp = true
				}
			}
			for space := range spaceSet {
				if !slices.Contains(spaces[space], role) {
					spaces[space] = append(spaces[space], role)
				}
			}
			if !ownApp && len(spaceSet) > 0 {
				info.Roles = slices.DeleteFunc(info.Roles, func(item string) bool { return item == role })
			}
		}
		out = append(out, info)
	}
	for _, space := range slices.Sorted(maps.Keys(spaces)) {
		if seen[space] || len(spaces[space]) == 0 {
			continue
		}
		roles := slices.Clone(spaces[space])
		slices.Sort(roles)
		out = append(out, AppInfo{
			ID: space, Title: SpaceTitle(space, titles), Version: "1",
			Reads: []string{}, Roles: roles, Capabilities: []platform.CapabilityInfo{},
			Inputs: []string{}, Uses: []string{}, Subscribes: []string{},
			Provides: []string{}, Consumes: []string{}, Emits: []platform.EffectKind{}, Interfaces: []platform.Interface{},
		})
	}
	return out
}

type ProtocolCall struct {
	Target         string          `json:"target"`
	IdempotencyKey string          `json:"idempotencyKey"`
	Payload        json.RawMessage `json:"payload"`
}

type MeView struct {
	TenantID    string                `json:"tenantId"`
	PrincipalID string                `json:"principalId"`
	Profile     platform.Member       `json:"profile"`
	Apps        []AppEntry            `json:"apps"`
	Tenants     []MeTenant            `json:"tenants"`
	Language    string                `json:"language"`
	Languages   []string              `json:"languages"`
	Preferred   string                `json:"preferred,omitempty"`
	Currency    string                `json:"currency"`
	Account     protocol.Account      `json:"account"`
	Tenant      protocol.TenantRecord `json:"tenant"`
	HostAdmin   bool                  `json:"hostAdmin,omitempty"`
}

// DeclaredSpaceTitles derives virtual namespace titles from installed apps.
func DeclaredSpaceTitles(definitions []platform.Definition) map[string]string {
	titles := map[string]string{}
	for _, d := range definitions {
		if d.Application == nil || d.Application.Title == "" {
			continue
		}
		name := d.Application.Name
		space, _, _ := strings.Cut(name, ".")
		if name == space || titles[space] == "" {
			titles[space] = d.Application.Title
		}
	}
	return titles
}
