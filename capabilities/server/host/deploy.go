package host

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	fileowner "platformserver/services/data/blobs"
	"platformserver/services/devops/packages"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	"platformserver/services/security/idp"
	"strings"
	"sync"
	"syscall"
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
	if d.IdentityProxy != "" {
		if d.Issuer == "" {
			return fmt.Errorf("-oidc-proxy requires -oidc-issuer")
		}
		if _, err := identityUpstream(d.IdentityProxy); err != nil {
			return err
		}
	}
	switch d.Profile {
	case "", "delivery":
		d.Profile = "delivery"
		if d.Data != "" || d.IDPKey != "" {
			return fmt.Errorf("-data and -idp-key belong to -profile lightweight; the delivery profile keeps -database, -files and -oidc-issuer")
		}
		return nil
	case "lightweight":
		if d.Data == "" {
			return fmt.Errorf("-profile lightweight needs -data <directory>: a lightweight host keeps its journal, file bytes and signing key there")
		}
		for name, set := range map[string]bool{"-database": d.Database != "", "-files": d.Files != "", "-oidc-issuer": d.Issuer != "",
			"-oidc-keys": d.Keys != "", "-oidc-proxy": d.IdentityProxy != "", "-project": d.Project} {
			if set {
				return fmt.Errorf("%s is not part of the lightweight profile: its state is the -data directory", name)
			}
		}
		return nil
	}
	return fmt.Errorf("unknown -profile %q: delivery, lightweight", d.Profile)
}

func (d *Deployment) lightweightIdP() (*idp.Local, error) {
	key, err := idp.LoadKey(d.idpKeyPath())
	if err != nil {
		return nil, err
	}
	return idp.NewLocal(key)
}

func (d *Deployment) makeKey() error {
	if _, err := os.Stat(d.idpKeyPath()); err == nil {
		return fmt.Errorf("idp: %s already exists; it is not replaced", d.idpKeyPath())
	}
	key, err := idp.NewKey()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.idpKeyPath()), 0o700); err != nil {
		return err
	}
	return idp.SaveKey(d.idpKeyPath(), key)
}

func (d *Deployment) lightweightState() (Journals, platform.FileStore, *idp.Local, error) {
	journal, err := d.openJournals(context.Background())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("journal: %w", err)
	}
	files, err := fileowner.NewLocalFiles(filepath.Join(d.Data, "files"))
	if err != nil {
		journal.Close()
		return nil, nil, nil, err
	}
	signer, err := d.lightweightIdP()
	if err != nil {
		journal.Close()
		return nil, nil, nil, err
	}
	return journal, files, signer, nil
}

func (d *Deployment) restoreTenants(ctx context.Context, j Journals, code string, tenants []*Tenant, restored map[string]int64, fresh map[string]bool) error {
	for _, t := range tenants {
		t.Store = j
		recoveryStarted := time.Now().UTC()
		var statePosition int64
		var stateActive bool
		if recovery, ok := j.(persistence.RuntimeStateRecovery); ok {
			var err error
			statePosition, stateActive, err = recovery.StatePosition(ctx, t.ID)
			if err != nil {
				return fmt.Errorf("read SQL state position %s: %w", t.ID, err)
			}
		}
		auditPage, auditErr := t.Log().QueryAuditEntries(platform.AuditQuery{Tenant: t.ID, Limit: 1})
		if auditErr == nil && len(auditPage.Entries) == 0 {
			if all, err := j.Entries(ctx, t.ID, 0); err == nil {
				t.backfillAudit(all)
			}
		}
		var after int64
		if d.SnapshotEvery > 0 {
			key, err := t.runtimeSnapshotKey(code)
			if err != nil {
				return fmt.Errorf("snapshot format %s: %w", t.ID, err)
			}
			seq, state, ok, err := j.Snapshot(ctx, t.ID, key)
			if err == nil && !ok {
				key, err = t.snapshotKey(code)
				if err == nil {
					seq, state, ok, err = j.Snapshot(ctx, t.ID, key)
				}
			}
			if err == nil && !ok && key != code {
				seq, state, ok, err = j.Snapshot(ctx, t.ID, code)
			}
			if err != nil {
				if errors.Is(err, errTenantSnapshot) {
					t.quarantine(err)
					log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
					continue
				}
				return fmt.Errorf("snapshot %s: %w", t.ID, err)
			}
			if ok && (!stateActive || seq <= statePosition) {
				if err := t.checkSnapshotCheckpoint(state, seq); err != nil {
					t.quarantine(err)
					log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
					continue
				}
				runtime, runtimeErr := runtimekernel.ReadCheckpoint(state)
				if runtimeErr != nil {
					t.quarantine(runtimeErr)
					continue
				}
				if runtime != nil {
					if _, err := j.Entries(ctx, t.ID, seq); err != nil {
						if !errors.Is(err, errTenantJournal) {
							return fmt.Errorf("read journal %s: %w", t.ID, err)
						}
						t.quarantine(err)
						log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
						continue
					}
					if j.Position(t.ID) == statePosition {
						if err := t.restoreRuntimeSnapshot(ctx, j, state, seq, statePosition, recoveryStarted); err != nil {
							t.quarantine(fmt.Errorf("SQL checkpoint recovery: %w", err))
							log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
							continue
						}
						latest, err := t.checkpointRuntime(ctx, j, code)
						if err != nil {
							t.quarantine(fmt.Errorf("SQL checkpoint save: %w", err))
							log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
							continue
						}
						restored[t.ID], fresh[t.ID] = latest, false
						log.Printf("restored %s from the snapshot at %d, then replayed %d entries with SQL records on demand", t.ID, seq, statePosition-seq)
						continue
					}
				} else {
					if err := t.recoverSnapshot(state, seq); err != nil {
						log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
						continue
					}
					after = seq
				}
			}
			restored[t.ID] = after
		}
		entries, err := j.Entries(ctx, t.ID, after)
		if err != nil {
			if errors.Is(err, errTenantJournal) {
				t.quarantine(err)
				log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
				continue
			}
			return fmt.Errorf("read journal %s: %w", t.ID, err)
		}
		var prefix *persistence.StateCheckpoint
		if stateActive && statePosition >= after && statePosition < j.Position(t.ID) {
			split := int(statePosition - after)
			if err := t.recoverEntries(entries[:split]); err != nil {
				log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
				continue
			}
			t.Lane.Lock()
			delta, err := t.runtimeStateDeltaLocked()
			t.Lane.Unlock()
			if err != nil {
				t.quarantine(fmt.Errorf("runtime state prefix: %w", err))
				log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
				continue
			}
			prefix = &persistence.StateCheckpoint{Position: statePosition, Delta: delta}
			if err := t.recoverEntries(entries[split:]); err != nil {
				log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
				continue
			}
		} else if err := t.recoverEntries(entries); err != nil {
			log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
			continue
		}
		if console, ok := t.App(controlowner.PlatformApp).(platform.IdentityRecovery); ok {
			console.CloseTokenSecretWindows()
		}
		fresh[t.ID] = after == 0 && len(entries) == 0
		if t.procs != nil {
			if err := t.procs.Check(); err != nil {
				t.quarantine(fmt.Errorf("process recovery: %w", err))
				log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
				continue
			}
		}
		t.flowArtifactRecoveryComplete(recoveryStarted)
		if after > 0 {
			log.Printf("restored %s from the snapshot at %d, then replayed %d entries", t.ID, after, len(entries))
		} else {
			log.Printf("replayed %d entries for %s", len(entries), t.ID)
		}
		if err := t.migrateRuntimeStateFrom(ctx, j, prefix); err != nil {
			t.quarantine(fmt.Errorf("runtime state: %w", err))
			log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
			continue
		}
		t.attachJournal(ctx, j)
		if _, ok := j.(persistence.StateSnapshotStorage); ok && d.SnapshotEvery > 0 {
			seq, err := t.checkpointRuntime(ctx, j, code)
			if err != nil {
				t.quarantine(fmt.Errorf("SQL checkpoint save: %w", err))
				log.Printf("quarantined %s: %s", t.ID, t.Fault.Load().Reason)
				continue
			}
			restored[t.ID] = seq
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
	_ = resources.Track("background observers", func(closing context.Context) error {
		cancel()
		done := make(chan struct{})
		go func() { background.Wait(); close(done) }()
		select {
		case <-done:
			return nil
		case <-closing.Done():
			return closing.Err()
		}
	})
	defer func() {
		closing, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		serveErr = errors.Join(serveErr, resources.Close(closing))
	}()
	if err := d.validate(); err != nil {
		return err
	}
	if d.NewKey {
		if err := d.makeKey(); err != nil {
			return err
		}
		log.Printf("made the signing key %s", d.idpKeyPath())
		return nil
	}
	var signer *idp.Local
	if d.Profile == "lightweight" {
		var err error
		if signer, err = d.lightweightIdP(); err != nil {
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
	if err := d.configurePersonalTokens(); err != nil {
		return err
	}
	for _, tenant := range tenants {
		tenant.services.StorageModule = d.storageModule()
	}
	if err := d.preparePackageIndex(tenants); err != nil {
		return err
	}
	flush := exportTelemetry(ctx, filepath.Base(os.Args[0]))
	defer flush(context.Background())
	var lightweightFiles platform.FileStore
	var files platform.FileStore
	switch {
	case d.Files != "":
		store, err := fileowner.NewS3Files(ctx, d.Files)
		if err != nil {
			return err
		}
		files = store
		for _, t := range tenants {
			t.Files = store
		}
		log.Printf("files in %s", d.Files)
	}
	code := CodeOf(tenants...)
	restored := map[string]int64{}
	fresh := map[string]bool{}
	switch {
	case d.Profile == "lightweight":
		var err error
		journal, lightweightFiles, signer, err = d.lightweightState()
		if err != nil {
			return err
		}
		files = lightweightFiles
		for _, t := range tenants {
			t.Files = lightweightFiles
		}
		log.Printf("journal and file bytes in %s", d.Data)
	case d.Database != "" || d.Storage != nil:
		var err error
		if journal, err = d.openJournals(ctx); err != nil {
			return fmt.Errorf("journal: %w", err)
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
		var mu sync.Mutex
		var entries []Entry
		appendEntry := func(e Entry) {
			mu.Lock()
			entries = append(entries, e)
			mu.Unlock()
		}
		if tenant.Record == nil {
			tenant.Record = appendEntry
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
	authenticate := Authenticate(func(token string) (string, bool) { return token, token != "" })
	var attest Attest
	if d.Issuer != "" {
		if d.Keys == "" {
			return fmt.Errorf("-oidc-keys is required with -oidc-issuer")
		}
		authenticate, attest = idp.OIDCProvider(d.Issuer, d.Keys)
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
		background.Add(1)
		go func() {
			defer background.Done()
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					snapshots.save(ctx, registry.Current(original.ID), false)
				}
			}
		}()
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
	host.HostAdmins = d.hostAdministrators()
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
	return d.listen(ctx, host, func(shutdown context.Context) error {
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

func (d *Deployment) listen(ctx context.Context, host *Host, atExit func(context.Context) error) error {
	handler, err := identityProxyHandler(d.IdentityProxy, host.Handler())
	if err != nil {
		return err
	}
	server := &http.Server{Addr: d.Addr, Handler: handler}
	stop, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	log.Printf("host on http://%s", d.Addr)
	select {
	case err := <-failed:
		return err
	case <-stop.Done():
	}
	shutdown, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	if err := server.Shutdown(shutdown); err != nil {
		return err
	}
	return atExit(shutdown)
}

func (d *Deployment) createTenant(req platform.CreateTenantRequest, by string, files platform.FileStore, open, seed func(*Tenant) error) (*Tenant, error) {
	now := Now()
	spec, tpl, err := d.spec(req, by, now)
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

func (d *Deployment) hostAdministrators() map[string]bool {
	out := map[string]bool{}
	for _, subject := range strings.Split(d.HostAdmins, ",") {
		if subject = strings.TrimSpace(subject); subject != "" {
			out[subject] = true
		}
	}
	return out
}

func (d *Deployment) configurePersonalTokens() error {
	if d.Issuer == "" {
		return nil
	}
	key := os.Getenv("PLATFORM_PERSONAL_TOKEN_KEY")
	if len(key) < 32 {
		return fmt.Errorf("OIDC delivery requires PLATFORM_PERSONAL_TOKEN_KEY of at least 32 bytes")
	}
	controlowner.UseTokenKey([]byte(key))
	return nil
}

func (d *Deployment) preparePackageIndex(tenants []*Tenant) error {
	index, err := packages.LoadPackageIndex(d.Packages)
	if err != nil {
		return err
	}
	d.packageIndex = index
	for _, tenant := range tenants {
		d.bindPackageIndex(tenant)
	}
	return nil
}

func (d *Deployment) bindPackageIndex(tenant *Tenant) {
	if console, ok := tenant.App(controlowner.PlatformApp).(packages.Catalog); ok && d.packageIndex != nil {
		console.BindPackageIndex(d.packageIndex)
	}
}

func (d *Deployment) rebuildTenant(id string) (*Tenant, error) {
	return d.composeRuntime(id, platform.DefaultRuntimeCapabilities(security.Standard{}).Selection())
}
