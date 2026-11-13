package host

import (
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/objects"
)

func (t *Tenant) objectOwner(store *records.Store) *objects.Owner {
	return &objects.Owner{
		Store:                  store,
		BaseStore:              t.Projection,
		TenantID:               t.ID,
		Admit:                  t.Admits,
		AuthorizationMember:    t.authorizationMember,
		Caller:                 t.objectCaller,
		Authorize:              t.authorizeFrom,
		AuthorizeDepth:         t.authorizeDepth,
		Visible:                t.visibleIn,
		VisibleForCaller:       t.visibleForCallerIn,
		MayRead:                t.mayReadIn,
		MayCallRead:            t.mayCallRead,
		Definitions:            t.Definitions,
		Entity:                 t.Entity,
		App:                    t.App,
		BindInterface:          t.bindInterfaceQuery,
		ValidateInterfaceQuery: t.checkInterfaceQueryFields,
		VisibleLink:            t.visibleLink,
		OwnedRelationTargets:   t.ownedRelationTargets,
		HasProcesses:           func() bool { return t.procs != nil },
		Follows:                t.objectFollows,
		Deliveries:             t.recordDeliveries,
		AuditPersonal:          t.objectPersonalRead,
		RecordSetBinding:       t.recordSetBinding,
		Claims:                 t.Claims.Facts,
	}
}

func (t *Tenant) objectCaller(m platform.Member, app string) platform.Caller {
	return platform.NewCaller(runtime{t: t, scoped: true}, m, app, false, false)
}

func (t *Tenant) objectFollows(m platform.Member, target string) bool {
	follow, ok := platform.Get[platform.RecordFollow](t.automation(platform.RelationsApp, false), platform.FollowID(m.ID, target))
	return ok && !follow.Archived
}

func (t *Tenant) objectPersonalRead(read platform.PersonalRead) {
	t.audit.ReadPersonal(read)
}
