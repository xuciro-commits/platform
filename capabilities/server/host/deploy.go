package host

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/records"
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
	Addr, Database, Issuer, Keys, Web string
	IdentityProxy                     string
	HostAdmins                        string
	TenantsFile, TemplatesDir         string
	Release                           int
	ReleaseDigest                     string
	specs                             *controlowner.Specs
	Files                             string
	Project                           bool
	SnapshotEvery                     int64
	Profile                           string
	Data                              string
	IDPKey                            string
	Mint                              string
	NewKey                            bool
	TokenTTL                          time.Duration
	Packages                          string
	packageIndex                      *platform.PackageIndex
	Rebuild                           func(id string) (*Tenant, error)
	RebuildWithCapabilities           func(id string, services platform.RuntimeCapabilities) (*Tenant, error)
	RuntimeProviders                  *platform.RuntimeProviders
	HostAuthorization                 platform.AuthorizationEngine
	Storage                           *JournalProvider
	runtimeContext                    context.Context
	background                        *sync.WaitGroup
	Seed                              func(t *Tenant, now time.Time) error
}

func Flags(addr string) *Deployment {
	d := &Deployment{}
	flag.StringVar(&d.Addr, "addr", addr, "listen address")
	flag.StringVar(&d.Database, "database", "", "PostgreSQL URL of the journal (empty: memory only)")
	flag.StringVar(&d.Issuer, "oidc-issuer", "", "OpenID issuer whose access tokens are accepted (empty: development tokens, the token is the subject)")
	flag.StringVar(&d.HostAdmins, "host-admins", "", "comma-separated authenticated subjects allowed to administer this host; empty: no host administrators")
	flag.StringVar(&d.Keys, "oidc-keys", "", "JWKS URL of the issuer, when the server reaches it on another address")
	flag.StringVar(&d.IdentityProxy, "oidc-proxy", "", "fixed identity service upstream exposed at /auth on this host")
	flag.StringVar(&d.Files, "files", "", "S3-compatible store of file bytes, http(s)://host:port/bucket, keys from PLATFORM_S3_ACCESS_KEY and PLATFORM_S3_SECRET_KEY (empty: memory)")
	flag.StringVar(&d.TenantsFile, "tenants", "", "JSON file with the host's tenants, their seats and starting settings; the host console appends to it (empty: the built-in development tenant)")
	flag.StringVar(&d.TemplatesDir, "templates", "", "directory of tenant templates the host console offers (empty: none)")
	flag.BoolVar(&d.Project, "project", false, "copy records into PostgreSQL tables per tenant for tools outside the host (needs -database)")
	flag.Int64Var(&d.SnapshotEvery, "snapshot-every", 10000, "save each tenant's state every this many journal entries and at shutdown, and start from the newest compatible snapshot (0: replay the whole journal)")
	flag.StringVar(&d.Web, "web", "", "directory of the workspace's build, served at / (empty: API only)")
	flag.StringVar(&d.Profile, "profile", "delivery", "delivery: PostgreSQL journal, S3 file bytes, external OpenID provider; lightweight: one -data directory, an internal signing key, no external services")
	flag.StringVar(&d.Data, "data", "", "directory of a lightweight host's state, journals and file bytes (required with -profile lightweight)")
	flag.StringVar(&d.IDPKey, "idp-key", "", "file with the lightweight host's token signing key (default: <data>/idp.key)")
	flag.BoolVar(&d.NewKey, "idp-new-key", false, "make a signing key when none exists, refuse to replace one, and stop")
	flag.StringVar(&d.Mint, "mint-token", "", "print an access token for this subject and stop (<user:email> or <client:id>, as the seats name them)")
	flag.DurationVar(&d.TokenTTL, "token-ttl", 12*time.Hour, "how long a minted token is accepted")
	flag.StringVar(&d.Packages, "packages", os.Getenv("PLATFORM_PACKAGES_DIR"), "directory of package descriptors the console offers (empty: no packages)")
	flag.IntVar(&d.Release, "release", 0, "the system release this binary serves (0: a development build)")
	flag.StringVar(&d.ReleaseDigest, "release-digest", "", "sha256 of this binary as the system release recorded it")
	return d
}

func (d *Deployment) idpKeyPath() string {
	if d.IDPKey != "" {
		return d.IDPKey
	}
	return filepath.Join(d.Data, "idp.key")
}

func (d *Deployment) validate() error {
	profile, err := idp.Profile{Name: d.Profile, Data: d.Data, KeyFile: d.IDPKey, Database: d.Database, Files: d.Files,
		Issuer: d.Issuer, Keys: d.Keys, Proxy: d.IdentityProxy, Project: d.Project}.Check()
	d.Profile = profile
	return err
}

func (d *Deployment) lightweightState() (Journals, platform.FileStore, *idp.Local, error) {
	journal, err := d.openJournals(context.Background())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("journal: %w", err)
	}
	files, err := fileowner.NewLocalFiles(filepath.Join(d.Data, "files"))
	if err == nil {
		var signer *idp.Local
		if signer, err = idp.LoadLocal(d.idpKeyPath()); err == nil {
			return journal, files, signer, nil
		}
	}
	journal.Close()
	return nil, nil, nil, err
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
	defer func() {
		closing, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		serveErr = errors.Join(serveErr, resources.Close(closing))
	}()
	if err := d.validate(); err != nil {
		return err
	}
	if d.NewKey {
		if err := idp.MakeKeyFile(d.idpKeyPath()); err != nil {
			return err
		}
		log.Printf("made the signing key %s", d.idpKeyPath())
		return nil
	}
	var signer *idp.Local
	if d.Profile == "lightweight" {
		var err error
		if signer, err = idp.LoadLocal(d.idpKeyPath()); err != nil {
			return err
		}
	}
	if d.Mint != "" {
		if signer == nil {
			return fmt.Errorf("-mint-token is the lightweight profile's; a delivery host signs in at its OpenID provider")
		}
		token, err := signer.Mint(d.Mint, d.TokenTTL, Now())
		if err != nil {
			return err
		}
		fmt.Println(token)
		return nil
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
		if journal, files, signer, err = d.lightweightState(); err != nil {
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
		if err := d.restoreRuntimeSelections(ctx, journal, code, tenants); err != nil {
			return err
		}
	}
	registry := newTenantRegistry(tenants)
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
	if pgPool, pg := journalPool(journal); journal != nil && d.Project && pg {
		for _, t := range tenants {
			if t.quarantined() {
				continue
			}
			p, err := records.ProjectSQL(ctx, pgPool, t.ID, t.Projection, t.readRecords)
			if err != nil {
				log.Printf("projection %s: %v", t.ID, err)
				continue
			}
			log.Printf("projected %s into schema %s (reader role %s)", t.ID, records.ProjectionSchema(t.ID), records.ReaderRole(t.ID))
			d.runProjection(registry, t, p)
		}
	}
	unregister := observeFrom(registry.List)
	_ = resources.Track("metrics callback", func(context.Context) error { return unregister() })
	_ = resources.Track("background work", runWorkFrom(registry.List))
	var snapshots *snapshotter
	watch := func(original *Tenant) {
		if snapshots == nil {
			return
		}
		snapshots.mu.Lock()
		snapshots.saved[original.ID] = restored[original.ID]
		snapshots.mu.Unlock()
		runtimekernel.Every(ctx, &background, 5*time.Second, func() bool {
			snapshots.save(ctx, registry.Current(original.ID), false)
			return true
		})
	}
	if journal != nil && d.SnapshotEvery > 0 {
		snapshots = &snapshotter{journal: journal, code: code, every: d.SnapshotEvery, saved: map[string]int64{}}
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
				snapshots.mu.Lock()
				snapshots.saved[id] = journal.Position(id)
				snapshots.mu.Unlock()
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
				snapshots.save(shutdown, t, true)
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
