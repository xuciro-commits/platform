package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"strconv"
	"strings"
	"time"
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
	return l.CheckSchema(parent, child)
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
	def := platform.Definition{Ref: ref, Source: "tenant", Version: h.app.Manifest().Version + ".link-" + strconv.Itoa(version), ContractVersion: 1, Requires: uniqueRefs([]platform.AssetRef{l.Parent, l.Child}), LinkType: &l, LinkVersions: map[string]platform.LinkType{}}
	i := slices.IndexFunc(h.t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
	if i >= 0 {
		old := h.t.definitions[i]
		if old.Source != "tenant" {
			return fmt.Errorf("link type is owned by code")
		}
		def.LinkVersions = maps.Clone(old.LinkVersions)
		if def.LinkVersions == nil {
			def.LinkVersions = map[string]platform.LinkType{}
		}
		if old.LinkType != nil {
			def.LinkVersions[old.Version] = *old.LinkType
		}
		if prior, ok := def.LinkVersions[def.Version]; ok && string(platform.Raw(prior)) != string(platform.Raw(l)) {
			return fmt.Errorf("link version bytes cannot change")
		}
	}
	def.LinkVersions[def.Version] = l
	if i >= 0 {
		h.t.definitions[i] = def
	} else {
		h.t.definitions = append(h.t.definitions, def)
	}
	slices.SortFunc(h.t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return nil
}

// TraverseLink follows exactly one retained relationship, after authorizing
// both its schema and its starting record. Original Records owns projection.
func (t *Tenant) TraverseLink(m platform.Member, binding platform.AssetBinding, direction, id string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	notFound := platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Link type or start record is unavailable")
	if binding.Ref.Kind != platform.AssetLinkType || binding.SourceVersion == "" || id == "" || (direction != "forward" && direction != "reverse") {
		return RecordPage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A link traversal needs a version, direction and start record")
	}
	var link *platform.LinkType
	for _, d := range t.Definitions(m) {
		if d.Ref == binding.Ref {
			if selected := d.LinkVersion(binding.SourceVersion); selected != nil {
				link = selected.LinkType
			}
			break
		}
	}
	if link == nil {
		return RecordPage{}, notFound
	}
	var domain []any
	if len(q.Domain) > 0 && json.Unmarshal(q.Domain, &domain) != nil {
		return RecordPage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid link query")
	}
	typ := link.Child.Name
	if direction == "forward" {
		if _, err := t.RecordOf(m, link.Parent.Name, id, now); err != nil {
			return RecordPage{}, notFound
		}
		domain = append(domain, []any{link.Via, "=", id})
	} else {
		view, err := t.RecordOf(m, link.Child.Name, id, now)
		if err != nil {
			return RecordPage{}, notFound
		}
		raw := platform.Raw(view.Record)
		var fields map[string]json.RawMessage
		var parent string
		if json.Unmarshal(raw, &fields) != nil {
			return RecordPage{}, notFound
		}
		if len(fields[link.Via]) == 0 && !link.Required {
			return RecordPage{Records: []any{}, Total: 0}, nil
		}
		if json.Unmarshal(fields[link.Via], &parent) != nil {
			return RecordPage{}, notFound
		}
		if parent == "" {
			return RecordPage{Records: []any{}, Total: 0}, nil
		}
		if _, err := t.RecordOf(m, link.Parent.Name, parent, now); err != nil {
			return RecordPage{}, notFound
		}
		typ = link.Parent.Name
		domain = append(domain, []any{"id", "=", parent})
	}
	q.Domain = platform.Raw(domain)
	return t.Records(m, typ, q, now)
}
