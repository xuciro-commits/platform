package host

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/kernel/transport"
	"platformserver/platform"
	fileowner "platformserver/services/data/blobs"
	"platformserver/services/devops/packages"
	"platformserver/services/resources/monitoring"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	"platformserver/services/security/idp"
	"sync"
	"time"
)

type Deployment struct {
	idp.Flags
	specs                   *controlowner.Specs
	packageIndex            *platform.PackageIndex
	Rebuild                 func(id string) (*Tenant, error)
	RebuildWithCapabilities func(id string, services platform.RuntimeCapabilities) (*Tenant, error)
	RuntimeProviders        *platform.RuntimeProviders
	HostAuthorization       platform.AuthorizationEngine
	Storage                 *persistence.JournalProvider
	runtimeContext          context.Context
	background              *sync.WaitGroup
	Seed                    func(t *Tenant, now time.Time) error
}

func Flags(addr string) *Deployment {
	d := &Deployment{}
	d.Register(addr)
	return d
}

func (d *Deployment) lightweightState() (Journals, platform.FileStore, error) {
	journal, err := d.openJournals(context.Background())
	if err != nil {
		return nil, nil, fmt.Errorf("journal: %w", err)
	}
	files, err := fileowner.NewLocalFiles(filepath.Join(d.Data, "files"))
	if err != nil {
		journal.Close()
		return nil, nil, err
	}
	return journal, files, nil
}

func (d *Deployment) restoreTenants(ctx context.Context, j Journals, code string, tenants []*Tenant, restored map[string]int64, fresh map[string]bool) error {
	for _, t := range tenants {
		t.Store = j
		if err := d.tenantRestore(ctx, t, j, code, restored, fresh).Run(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (d *Deployment) Serve(tenants ...*Tenant) (serveErr error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var journal Journals
	defer func() {
		if journal != nil {
			journal.Close()
		}
	}()
	var resources platform.Resources
	var background sync.WaitGroup
	d.runtimeContext, d.background = ctx, &background
	defer func() { d.runtimeContext, d.background = nil, nil }()
	_ = resources.Track("background observers", runtimekernel.Drain(cancel, &background))
	defer func() { serveErr = runtimekernel.CloseAll(&resources, serveErr) }()
	if err := d.Validate(); err != nil {
		return err
	}
	signer, done, err := d.Startup().Run(Now())
	if err != nil || done {
		return err
	}
	if err := controlowner.ConfigurePersonalTokens(d.Issuer); err != nil {
		return err
	}
	for _, tenant := range tenants {
		tenant.services.StorageModule = d.storageModule()
	}
	index, err := packages.LoadPackageIndex(d.Packages)
	if err != nil {
		return err
	}
	d.packageIndex = index
	for _, tenant := range tenants {
		d.bindPackageIndex(tenant)
	}
	flush := monitoring.ExportTelemetry(ctx, filepath.Base(os.Args[0]))
	defer flush(context.Background())
	var files platform.FileStore
	if d.Files != "" {
		store, err := fileowner.NewS3Files(ctx, d.Files)
		if err != nil {
			return err
		}
		files = store
		log.Printf("files in %s", d.Files)
	}
	code := CodeOf(tenants...)
	restored := map[string]int64{}
	fresh := map[string]bool{}
	switch {
	case d.Profile == "lightweight":
		if journal, files, err = d.lightweightState(); err != nil {
			return err
		}
		log.Printf("journal and file bytes in %s", d.Data)
	case d.Database != "" || d.Storage != nil:
		if journal, err = d.openJournals(ctx); err != nil {
			return fmt.Errorf("journal: %w", err)
		}
	}
	if files != nil {
		for _, t := range tenants {
			t.Files = files
		}
	}
	if journal != nil {
		if err := d.restoreTenants(ctx, journal, code, tenants, restored, fresh); err != nil {
			return err
		}
		if err := d.restoreRuntimeSelections(ctx, journal, tenants); err != nil {
			return err
		}
	}
	registry := controlowner.NewRegistry(tenants, func(t *Tenant) string { return t.ID }, func(t *Tenant) bool { return t.quarantined() })
	memoryJournal := func(tenant *Tenant) {
		if tenant.Record == nil {
			tenant.Record = func(Entry) {}
		}
	}
	seed := func(t *Tenant) error {
		if d.Seed != nil && !t.quarantined() && (journal == nil || fresh[t.ID]) {
			if err := d.Seed(t, time.Now()); err != nil {
				return fmt.Errorf("seed %s: %w", t.ID, err)
			}
			log.Printf("seeded %s", t.ID)
		}
		return nil
	}
	if journal == nil {
		for _, tenant := range tenants {
			memoryJournal(tenant)
		}
	}
	for _, t := range tenants {
		if err := seed(t); err != nil {
			return err
		}
		if !t.quarantined() {
			if err := t.initializeResources(Now()); err != nil {
				return fmt.Errorf("resource directory %s: %w", t.ID, err)
			}
		}
	}
	authenticate, attest, err := idp.Select(d.Issuer, d.Keys)
	if err != nil {
		return err
	}
	for _, t := range tenants {
		if journal == nil || t.quarantined() {
			continue
		}
		p, err := d.project(ctx, journal, t)
		if err != nil {
			log.Printf("projection %s: %v", t.ID, err)
		} else if p != nil {
			log.Printf("projected %s into schema %s (reader role %s)", t.ID, records.ProjectionSchema(t.ID), records.ReaderRole(t.ID))
			d.runProjection(registry, t, p)
		}
	}
	unregister := observeFrom(registry.List)
	_ = resources.Track("metrics callback", func(context.Context) error { return unregister() })
	_ = resources.Track("background work", runWorkFrom(registry.List))
	var snapshots *runtimekernel.SnapshotSaver
	watch := func(original *Tenant) {
		if snapshots == nil {
			return
		}
		snapshots.Note(original.ID, restored[original.ID])
		runtimekernel.Every(ctx, &background, 5*time.Second, func() bool {
			snapshots.Save(ctx, registry.Current(original.ID).snapshotSource(ctx, journal, code), false)
			return true
		})
	}
	if journal != nil && d.SnapshotEvery > 0 {
		snapshots = runtimekernel.NewSnapshotSaver(journal, d.SnapshotEvery)
		for _, original := range tenants {
			watch(original)
		}
	}
	host := NewHost(authenticate, tenants...)
	host.RuntimeProviders = d.providers()
	if d.HostAuthorization != nil {
		host.AuthorizationEngine = d.HostAuthorization
	}
	host.SecondFactor = attest
	host.HostAdmins = controlowner.HostAdministrators(d.HostAdmins)
	host.tenantsFrom = registry.List
	host.Templates = d.Templates
	if d.Rebuild != nil {
		var createMu sync.Mutex
		host.CreateTenant = func(req platform.CreateTenantRequest, by string) (*Tenant, error) {
			createMu.Lock()
			defer createMu.Unlock()
			t, err := d.createTenant(req, by, files, func(t *Tenant) error {
				if journal == nil {
					memoryJournal(t)
					return nil
				}
				return d.restoreTenants(ctx, journal, code, []*Tenant{t}, restored, fresh)
			}, seed)
			if err != nil {
				return nil, err
			}
			registry.Add(t)
			watch(t)
			return t, nil
		}
	}
	if journal != nil && d.Rebuild != nil {
		var recoveryMu sync.Mutex
		host.Recover = func(ctx context.Context, id string) error {
			recoveryMu.Lock()
			defer recoveryMu.Unlock()
			if err := d.retryTenant(ctx, journal, registry, code, id); err != nil {
				return err
			}
			if snapshots != nil {
				snapshots.Note(id, journal.Position(id))
			}
			return nil
		}
	}
	host.Web, host.Issuer, host.Client, host.Development = d.Web, d.Issuer, "platform-web", d.Issuer == ""
	host.Release, host.ReleaseDigest = d.Release, d.ReleaseDigest
	if signer != nil {
		host.SignWith(signer, d.TokenTTL)
	}
	handler, err := idp.IdentityProxyHandler(d.IdentityProxy, host.Handler())
	if err != nil {
		return err
	}
	return transport.Serve(ctx, d.Addr, handler, func(shutdown context.Context) error {
		for _, t := range registry.List() {
			t.FlushObservations()
		}
		if err := resources.Close(shutdown); err != nil {
			return err
		}
		cancel()
		if snapshots != nil {
			for _, t := range registry.List() {
				snapshots.Save(shutdown, t.snapshotSource(shutdown, journal, code), true)
			}
		}
		return nil
	})
}

func (d *Deployment) createTenant(req platform.CreateTenantRequest, by string, files platform.FileStore, open, seed func(*Tenant) error) (*Tenant, error) {
	now := Now()
	spec, tpl, err := controlowner.NewTenantSpec(d.Templates, req, by, now)
	if err != nil {
		return nil, err
	}
	if d.specs == nil {
		d.specs = controlowner.NewSpecs(d.TenantsFile)
	}
	if err := d.specs.Add(spec); err != nil {
		return nil, err
	}
	t, err := d.rebuildTenant(spec.ID)
	if err != nil {
		return nil, err
	}
	t.Files = files
	if err := open(t); err != nil {
		return nil, err
	}
	if err := seed(t); err != nil {
		return nil, err
	}
	if err := firstDecisions(t, spec, tpl, now); err != nil {
		return nil, err
	}
	log.Printf("created tenant %s for %s", spec.ID, by)
	return t, nil
}

func (d *Deployment) bindPackageIndex(tenant *Tenant) {
	if console, ok := tenant.App(controlowner.PlatformApp).(packages.Catalog); ok && d.packageIndex != nil {
		console.BindPackageIndex(d.packageIndex)
	}
}

func (d *Deployment) rebuildTenant(id string) (*Tenant, error) {
	return d.composeRuntime(id, platform.DefaultRuntimeCapabilities(security.Standard{}).Selection())
}
