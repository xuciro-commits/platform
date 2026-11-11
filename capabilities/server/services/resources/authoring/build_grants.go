package build

import (
	"slices"
	"strings"

	"platformserver/platform"
)

func HoldsIndependentRole(m platform.Member, roles ...string) bool {
	held := false
	for _, g := range m.Grants {
		if g.App != ID {
			continue
		}
		held = true
		if !strings.HasPrefix(g.By, "project:") && slices.Contains(roles, g.Role) {
			return true
		}
	}
	return !held && slices.Contains(roles, m.Roles[ID])
}

func ProjectDelegated(m platform.Member) bool {
	return slices.ContainsFunc(m.Grants, func(g platform.Grant) bool { return g.App == ID && strings.HasPrefix(g.By, "project:") }) && !HoldsIndependentRole(m, Builder)
}
