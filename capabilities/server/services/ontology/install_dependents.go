package ontology

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

type InstallDraft struct {
	Reset        func()
	Install      func() error
	Types        func() []platform.EntityInfo
	CheckSets    func(info platform.EntityInfo, engine platform.RuleEngine) error
	Entity       func(typ string) (platform.EntityInfo, bool)
	App          func(id string) platform.App
	LinkType     func(owner string, l platform.LinkType) error
	Query        func(owner string, q platform.NamedQuery) error
	Function     func(owner string, f platform.AIFunction) error
	CheckQueries func(p platform.Page) error
	InstallPage  func(owner platform.App, p platform.Page) error
}

type InstallDependents struct {
	App           string
	Entity        platform.Entity
	Actions       []platform.Action
	GeneratedPage string
	Engine        platform.RuleEngine
	Definitions   []platform.Definition
	Bindings      map[string]string
	Provisions    func(provider string) []platform.Provision
	Begin         func() InstallDraft
}

func (c InstallDependents) Validate() error {
	e := c.Entity
	for protocol, provider := range c.Bindings {
		if provider != c.App {
			continue
		}
		belongs := false
		for _, provision := range c.Provisions(provider) {
			for _, schema := range provision.Actions {
				belongs = belongs || provision.Protocol.ID() == protocol && strings.HasPrefix(schema, e.Type+".")
			}
		}
		if belongs && !slices.ContainsFunc(e.Protocols, func(p platform.Provision) bool { return p.Protocol.ID() == protocol }) {
			return fmt.Errorf("protocol %s is bound to this object; retain its provider declaration", protocol)
		}
	}
	draft := c.Begin()
	draft.Reset()
	if err := draft.Install(); err != nil {
		return err
	}
	for _, et := range draft.Types() {
		if err := draft.CheckSets(et, c.Engine); err != nil {
			return err
		}
		if err := platform.CheckConstraints(et, draft.Entity, true, c.Engine); err != nil {
			return err
		}
	}
	for _, def := range c.Definitions {
		if def.LinkType != nil {
			if err := draft.LinkType(def.Ref.App, *def.LinkType); err != nil {
				return err
			}
			for _, l := range def.LinkVersions {
				if err := draft.LinkType(def.Ref.App, l); err != nil {
					return err
				}
			}
		}
		if def.Application != nil {
			for _, q := range def.Application.Queries {
				if q.Object.Name == e.Type {
					if err := draft.CheckQueries(def.Application.QueryPage()); err != nil {
						return err
					}
					break
				}
			}
		}
		if def.Query != nil && def.Query.Object == e.Type {
			if err := draft.Query(def.Ref.App, *def.Query); err != nil {
				return err
			}
			for _, q := range def.QueryVersions {
				if err := draft.Query(def.Ref.App, q); err != nil {
					return err
				}
			}
		}
		if def.Function != nil && def.Function.Object == e.Type {
			if err := draft.Function(def.Ref.App, *def.Function); err != nil {
				return err
			}
		}
	}
	removedAction := func(ref platform.AssetRef) bool {
		owned := ref.App == c.App && slices.ContainsFunc(c.Definitions, func(d platform.Definition) bool {
			return d.Ref == ref && d.Action != nil && d.Action.Target == e.Type
		})
		return owned && !slices.ContainsFunc(c.Actions, func(a platform.Action) bool { return a.Schema == ref.Name })
	}
	for _, def := range c.Definitions {
		if def.Ref.Kind != platform.AssetPage || def.Page == nil ||
			def.Ref.App == c.App && def.Ref.Name == c.GeneratedPage {
			continue
		}
		p, err := def.Page.Safety()
		if err != nil {
			return err
		}
		if p.Object.Name != e.Type && !slices.ContainsFunc(p.QueryReferences(), func(ref platform.AssetRef) bool { return ref.Kind == platform.AssetObject && ref.Name == e.Type }) && !slices.ContainsFunc(p.DataBindings, func(s platform.PageDataBinding) bool {
			return s.Object.Name == e.Type
		}) {
			continue
		}
		for _, trigger := range p.ActionTriggers {
			for _, ref := range trigger.Actions {
				if removedAction(ref) {
					return fmt.Errorf("page %s: removed action %s", p.Name, ref.Name)
				}
			}
		}
		owner := draft.App(def.Ref.App)
		if owner == nil {
			return fmt.Errorf("page %s has no owner %s", def.Ref, def.Ref.App)
		}
		if err := draft.InstallPage(owner, p); err != nil {
			return fmt.Errorf("release dependent %s: %w", def.Ref, err)
		}
	}
	return nil
}
