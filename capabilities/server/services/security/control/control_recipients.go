package control

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"platformserver/platform"
)

type RecipientBook struct {
	Identity  IdentityOwner
	Directory platform.Directory
}

func (r RecipientBook) Members(app string, to []platform.Recipient, date string) []string {
	return Recipients(app, to, date,
		func(app, role string) []string {
			if r.Identity != nil {
				return r.Identity.Holding(app, role)
			}
			return nil
		},
		func(structure, unit, role, date string) []string {
			if r.Directory == nil {
				return nil
			}
			return r.Directory.Holders(structure, unit, role, date)
		})
}

func (r RecipientBook) Reach(members []string) map[string]platform.Reach {
	out := map[string]platform.Reach{}
	if r.Identity == nil {
		return out
	}
	for _, member := range members {
		out[member] = r.Identity.Reach(member)
	}
	return out
}

func Recipients(app string, to []platform.Recipient, date string, holding func(app, role string) []string, holders func(structure, unit, role, date string) []string) []string {
	var members []string
	add := func(ms ...string) {
		for _, m := range ms {
			if !slices.Contains(members, m) {
				members = append(members, m)
			}
		}
	}
	for _, r := range to {
		if r.Member != "" {
			add(r.Member)
		}
		if r.AppRole != "" {
			add(holding(app, r.AppRole)...)
		}
		if r.Unit != "" {
			add(holders(r.Structure, r.Unit, r.Role, date)...)
		}
	}
	return members
}

func LookupSecret(provided func(string) ([]byte, bool), name string) ([]byte, bool) {
	if provided != nil {
		return provided(name)
	}
	if dir := os.Getenv("PLATFORM_SECRETS_DIR"); dir != "" {
		if raw, err := os.ReadFile(filepath.Join(dir, filepath.Base(name))); err == nil {
			return bytes.TrimSpace(raw), true
		}
	}
	v, ok := os.LookupEnv("PLATFORM_SECRET_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(name)))
	return []byte(v), ok && v != ""
}
