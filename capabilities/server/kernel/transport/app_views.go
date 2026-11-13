package transport

import (
	"cmp"
	"slices"

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

func AppEntries(manifests []platform.Manifest, roles map[string]string) []AppEntry {
	out := []AppEntry{}
	for _, m := range manifests {
		if role := roles[m.ID]; role != "" {
			out = append(out, AppEntry{ID: m.ID, Title: cmp.Or(m.Title, m.ID), Role: role})
		}
	}
	return out
}

func AppInfos(manifests []platform.Manifest) []AppInfo {
	out := []AppInfo{}
	for _, m := range manifests {
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
		}
		out = append(out, info)
	}
	return out
}
