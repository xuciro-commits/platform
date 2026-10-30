package platformserver

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	pageowner "platformserver/studio/pages"
	releaseowner "platformserver/studio/releases"
	"slices"
	"strconv"
)

func (h hostView) ValidateInstallLinkType(l platform.LinkType) error {
	parent, ok := h.t.entity(l.Parent.Name)
	if !ok {
		return fmt.Errorf("link parent is unavailable")
	}
	child, ok := h.t.entity(l.Child.Name)
	if !ok || child.App != h.app.Manifest().ID {
		return fmt.Errorf("link type needs its child owner's object")
	}
	if err := l.CheckSchema(parent, child); err != nil {
		return err
	}
	if l.Storage == "edge" {
		h.t.records.Lock()
		defer h.t.records.Unlock()
		violations, err := ownedRelationViolations(h.t.records, l)
		if err != nil {
			return err
		}
		if len(violations) > 0 {
			return fmt.Errorf("owned relationship %s violates %s", l.Name, violations[0].reason)
		}
		return nil
	}
	h.t.records.Lock()
	defer h.t.records.Unlock()
	if l.Cardinality == "one-to-one" {
		if err := h.t.records.ValidateUniqueLinkLocked(l); err != nil {
			return err
		}
	}
	if l.DeletePolicy == "restrict-active" {
		return h.t.records.ValidateArchiveLinkLocked(l)
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
	def, err := h.t.describeDefinition(def)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(h.t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if i >= 0 {
		old := h.t.definitions[i]
		if old.Source != "tenant" {
			return fmt.Errorf("link type is owned by code")
		}
		var ok bool
		def.LinkVersions, ok = releaseowner.RetainedDefinitionVersions(old.LinkVersions, old.Version, old.LinkType, def.Version, l)
		if !ok {
			return fmt.Errorf("link version bytes cannot change")
		}
	}
	if err := h.t.records.InstallLinkConstraint(l); err != nil {
		return err
	}
	def.LinkVersions[def.Version] = l
	h.t.storeDefinition(i, def)
	return nil
}

func (t *Tenant) visibleLink(m platform.Member, binding platform.AssetBinding) *platform.LinkType {
	if selected := pageowner.QuerySourceDefinition(t.Definitions(m), &binding); selected != nil {
		return selected.LinkType
	}
	return nil
}
func (t *Tenant) traversalAround(store *records.Store, m platform.Member, typ string, q platform.Query) (*platform.SearchAround, *kernel.Error) {
	edge := q.Traversal
	if edge == nil {
		return nil, nil
	}
	if store != t.records {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Link traversal is unavailable in a staged read")
	}
	if edge.Binding.Ref.Kind != platform.AssetLinkType || edge.Binding.SourceVersion == "" || edge.ID == "" || (edge.Direction != "forward" && edge.Direction != "reverse") {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A link traversal needs a version, direction and start record")
	}
	link := t.visibleLink(m, edge.Binding)
	if link == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Link type or start record is unavailable")
	}
	target := link.Child.Name
	if edge.Direction == "reverse" {
		target = link.Parent.Name
	}
	if typ != target {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Link target object does not match its direction")
	}
	var domain []any
	if len(q.Domain) > 0 && json.Unmarshal(q.Domain, &domain) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid link query")
	}
	return &platform.SearchAround{From: platform.ObjectSet{IDs: []string{edge.ID}, Archived: true}, Link: edge.Binding, Direction: edge.Direction}, nil
}
