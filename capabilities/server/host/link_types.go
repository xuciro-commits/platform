package host

import (
	"fmt"
	"platformserver/kernel/records"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/objects"
	"strconv"
)

func (h hostView) ValidateInstallLinkType(l platform.LinkType) error {
	parent, ok := h.t.Entity(l.Parent.Name)
	if !ok {
		return fmt.Errorf("link parent is unavailable")
	}
	child, ok := h.t.Entity(l.Child.Name)
	if !ok || child.App != h.app.Manifest().ID {
		return fmt.Errorf("link type needs its child owner's object")
	}
	if err := l.CheckSchema(parent, child); err != nil {
		return err
	}
	if l.Storage == "edge" {
		h.t.Projection.Lock()
		defer h.t.Projection.Unlock()
		violations, err := records.RelationViolations(h.t.Projection, l)
		if err != nil {
			return err
		}
		if len(violations) > 0 {
			return fmt.Errorf("owned relationship %s violates %s", l.Name, violations[0].Reason)
		}
		return nil
	}
	h.t.Projection.Lock()
	defer h.t.Projection.Unlock()
	if l.Cardinality == "one-to-one" {
		if err := h.t.Projection.ValidateUniqueLinkLocked(l); err != nil {
			return err
		}
	}
	if l.DeletePolicy == "restrict-active" {
		return h.t.Projection.ValidateArchiveLinkLocked(l)
	}
	return nil
}
func (h hostView) InstallLinkType(c platform.Caller, l platform.LinkType, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if version < 1 || version > 64 {
		return fmt.Errorf("link type needs a retained version")
	}
	if err := h.ValidateInstallLinkType(l); err != nil {
		return err
	}
	ref := platform.AssetRef{App: h.app.Manifest().ID, Kind: platform.AssetLinkType, Name: l.Name}
	def := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".link-" + strconv.Itoa(version), ContractVersion: l.Contract(), LinkType: &l, LinkVersions: map[string]platform.LinkType{}}
	def, err := h.t.describer().DescribeDefinition(def)
	if err != nil {
		return err
	}
	i, versions, err := releaseowner.RetainVersion(h.t.definitions, ref, "link type is owned by code", "link version bytes cannot change", def.Version, l, func(old platform.Definition) (map[string]platform.LinkType, *platform.LinkType) {
		return old.LinkVersions, old.LinkType
	})
	if err != nil {
		return err
	}
	if err := h.t.Projection.InstallLinkConstraint(l); err != nil {
		return err
	}
	def.LinkVersions = versions
	h.t.storeDefinition(i, def)
	return nil
}

func (t *Tenant) visibleLink(m platform.Member, binding platform.AssetBinding) *platform.LinkType {
	if selected := objects.QuerySourceDefinition(t.Definitions(m), &binding); selected != nil {
		return selected.LinkType
	}
	return nil
}
