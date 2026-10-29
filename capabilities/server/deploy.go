package platformserver

import fileowner "platformserver/engine/resources"

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"platformserver/engine/integrations"
	"platformserver/idp"
	"platformserver/journal"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"platformserver/platform/authz"
	"platformserver/studio/packages"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Deployment is how a host binary runs: in memory with
// development tokens, or with a PostgreSQL journal and an OpenID provider.
type Deployment struct {
	Addr, Database, Issuer, Keys, Web string
	IdentityProxy                     string // explicit upstream for the public /auth path
	HostAdmins                        string // explicit authenticated subjects; tenant roles do not imply host authority
	// TenantsFile lists the host's tenants with their seats and starting
	// settings; the console appends to it. Empty: the
	// binary's development tenant. TemplatesDir holds tenant templates.
	TenantsFile, TemplatesDir string
	system                    TenantSpec
	Release                   int
	ReleaseDigest             string
	specs                     *specs
	Files                     string // http(s)://host:port/bucket of an S3-compatible store; empty: memory
	Project                   bool
	SnapshotEvery             int64
	// Profile is which infrastructure the host runs on:
	// "delivery" keeps the PostgreSQL journal, the S3 file bytes and an
	// external OpenID provider; "lightweight" keeps everything in one data
	// directory — a local journal file, local file bytes and the host's own
	// signing key. The profile is declared, never inferred: a host missing a
	// dependency of its profile stops instead of falling back to another.
	Profile string
	// Data is the lightweight profile's directory.
	Data string
	// IDPKey is the lightweight profile's signing key file (default
	// <data>/idp.key). Mint, with a subject, prints a token for that subject
	// and stops; TokenTTL is how long such a token lasts.
	IDPKey   string
	Mint     string
	NewKey   bool
	TokenTTL time.Duration
	// Packages is a directory of package descriptors: the
	// index the console lists and prechecks. Empty: no packages offered.
	Packages     string
	packageIndex *platform.PackageIndex
	// Rebuild composes a fresh, unstarted tenant with the same durable app and
	// connector declarations. Required for an in-process recovery retry.
	Rebuild                 func(id string) (*Tenant, error)
	RebuildWithCapabilities func(id string, services RuntimeCapabilities) (*Tenant, error)
	RuntimeProviders        *RuntimeProviders
	HostAuthorization       authz.Engine
	Storage                 *JournalProvider
	runtimeContext          context.Context
	background              *sync.WaitGroup
	// Seed gives a tenant whose journal is empty its first data, through
	// ordinary decisions journaled like any other (in memory: every start).
	Seed func(t *Tenant, now time.Time) error
}

// Flags registers the deployment flags on the default flag set.
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

// idpKeyPath is where the lightweight profile's signing key lives.
func (d *Deployment) idpKeyPath() string {
	if d.IDPKey != "" {
		return d.IDPKey
	}
	return filepath.Join(d.Data, "idp.key")
}

// validate settles the profile before anything is opened: the profile is
// explicit, its dependencies are required, and flags of the other profile are
// refused rather than ignored.
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

// idp, on a lightweight host, is the provider it signs and verifies tokens
// with. The seats served on /v1/sign-in are signed too, so no token of an
// unsigned form is ever accepted.
func (d *Deployment) lightweightIdP() (*idp.Local, error) {
	key, err := idp.LoadKey(d.idpKeyPath())
	if err != nil {
		return nil, err
	}
	return idp.NewLocal(key)
}

// makeKey writes a fresh signing key, refusing to replace one that exists: a
// replaced key would sign out every token already minted.
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

// lightweightState opens what a lightweight host runs on: its journal, its
// file bytes and its signing key, all inside -data.
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

// restoreTenants brings every tenant to the journal's state before the host
// serves: the newest compatible snapshot when there is one, then the entries
// after it. Both profiles run this same path: the lightweight
// profile's journal is a file, not a second set of semantics, so a restart
// replays and a damaged journal quarantines exactly as it does on PostgreSQL.
func (d *Deployment) restoreTenants(ctx context.Context, j Journals, code string, tenants []*Tenant, restored map[string]int64, fresh map[string]bool) error {
	ordered := slices.Clone(tenants)
	slices.SortStableFunc(ordered, func(a, b *Tenant) int {
		if a.ID == "system" && b.ID != "system" {
			return -1
		}
		if b.ID == "system" && a.ID != "system" {
			return 1
		}
		return 0
	})
	(&Host{}).bindSystemDefinitions(ordered)
	for _, t := range ordered {
		recoveryStarted := time.Now().UTC()
		var statePosition int64
		var stateActive bool
		if recovery, ok := j.(journal.RuntimeStateRecovery); ok {
			var err error
			statePosition, stateActive, err = recovery.StatePosition(ctx, t.ID)
			if err != nil {
				return fmt.Errorf("read SQL state position %s: %w", t.ID, err)
			}
		}
		legacyState := false
		if upgrade, ok := j.(journal.RuntimeStateUpgrade); ok {
			var err error
			legacyState, err = upgrade.StateNeedsUpgrade(ctx, t.ID)
			if err != nil {
				return fmt.Errorf("read SQL state format %s: %w", t.ID, err)
			}
		}
		var after int64
		if d.SnapshotEvery > 0 && !legacyState {
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
					log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
					continue
				}
				return fmt.Errorf("snapshot %s: %w", t.ID, err)
			}
			if ok && (!stateActive || seq <= statePosition) {
				if err := t.checkSnapshotCheckpoint(state, seq); err != nil {
					t.quarantine(err)
					log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
					continue
				}
				runtime, runtimeErr := runtimeCheckpoint(state)
				if runtimeErr != nil {
					t.quarantine(runtimeErr)
					continue
				}
				if runtime != nil {
					// A retained older writer may have appended accepted results
					// after SQL stopped advancing. Reconstruct that legacy prefix
					// independently below; never apply latest SQL as its past.
					if _, err := j.Entries(ctx, t.ID, seq); err != nil {
						if !errors.Is(err, errTenantJournal) {
							return fmt.Errorf("read journal %s: %w", t.ID, err)
						}
						t.quarantine(err)
						log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
						continue
					}
					if j.Position(t.ID) == statePosition {
						if err := t.restoreRuntimeSnapshot(ctx, j, state, seq, statePosition, recoveryStarted); err != nil {
							t.quarantine(fmt.Errorf("SQL checkpoint recovery: %w", err))
							log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
							continue
						}
						latest, err := t.checkpointRuntime(ctx, j, code)
						if err != nil {
							t.quarantine(fmt.Errorf("SQL checkpoint save: %w", err))
							log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
							continue
						}
						restored[t.ID], fresh[t.ID] = latest, false
						log.Printf("restored %s from the snapshot at %d, then replayed %d entries with SQL records on demand", t.ID, seq, statePosition-seq)
						continue
					}
				} else {
					if err := t.recoverSnapshot(state, seq); err != nil {
						log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
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
				log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
				continue
			}
			return fmt.Errorf("read journal %s: %w", t.ID, err) // a database error is not tenant-local
		}
		var prefix *journal.StateCheckpoint
		if stateActive && statePosition >= after && statePosition < j.Position(t.ID) {
			split := int(statePosition - after)
			if err := t.recoverEntries(entries[:split]); err != nil {
				log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
				continue
			}
			t.mu.Lock()
			delta, err := t.runtimeStateDeltaLocked()
			t.mu.Unlock()
			if err != nil {
				t.quarantine(fmt.Errorf("runtime state prefix: %w", err))
				log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
				continue
			}
			prefix = &journal.StateCheckpoint{Position: statePosition, Delta: delta}
			acceptedOnly := true
			for _, entry := range entries[split:] {
				acceptedOnly = acceptedOnly && entry.Kind == "accepted-result"
			}
			if !acceptedOnly {
				t.quarantine(fmt.Errorf("runtime state recovery suffix contains a legacy input"))
				log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
				continue
			}
			if err := t.recoverEntries(entries[split:]); err != nil {
				log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
				continue
			}
		} else if err := t.recoverEntries(entries); err != nil {
			log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
			continue
		}
		if console, ok := t.app(controlowner.PlatformApp).(platform.IdentityRecovery); ok {
			console.CloseTokenSecretWindows()
		}
		fresh[t.ID] = after == 0 && len(entries) == 0
		if t.procs != nil {
			if err := t.procs.Check(); err != nil { // running instances need their flow's version
				t.quarantine(fmt.Errorf("process recovery: %w", err))
				log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
				continue
			}
		}
		t.flowArtifactRecoveryComplete(recoveryStarted)
		if after > 0 {
			log.Printf("restored %s from the snapshot at %d, then replayed %d entries", t.ID, after, len(entries))
		} else {
			log.Printf("replayed %d entries for %s", len(entries), t.ID)
		}
		// An input the journal did not take is never answered; the client's
		// outbox resends it after the restart has replayed the rest.
		if err := t.migrateRuntimeStateFrom(ctx, j, prefix); err != nil {
			t.quarantine(fmt.Errorf("runtime state: %w", err))
			log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
			continue
		}
		t.attachJournal(ctx, j)
		// Upgrade the original full-row checkpoint once before serving. An
		// unchanged tenant at the prior saved position must not load every row
		// again merely because its periodic snapshotter has no new entries.
		if _, ok := j.(journal.StateSnapshotStorage); ok && d.SnapshotEvery > 0 {
			seq, err := t.checkpointRuntime(ctx, j, code)
			if err != nil {
				t.quarantine(fmt.Errorf("SQL checkpoint save: %w", err))
				log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
				continue
			}
			restored[t.ID] = seq
		}
	}
	return nil
}

// Serve replays each tenant's journal, from its newest snapshot of this code
// when there is one, then records into it (fail-stop), seeds a tenant whose
// journal was empty, runs the tenants' owned
// work every second, saves snapshots as the journal grows and at
// shutdown, and serves until SIGINT or SIGTERM.
func (d *Deployment) Serve(tenants ...*Tenant) (serveErr error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var journal Journals
	// Work and observers finish before their shared store closes, including
	// initialization and listener failures that bypass normal HTTP shutdown.
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
	flush := exportTelemetry(ctx, filepath.Base(os.Args[0])) // traces and metrics, when an OTLP endpoint is set
	defer flush(context.Background())
	var lightweightFiles platform.FileStore // the lightweight profile's bytes open with its journal, all in -data
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
	restored := map[string]int64{} // each tenant's snapshot position at start-up, 0 without one
	fresh := map[string]bool{}     // tenants whose journal was empty
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
	// The development host still uses the accepted-result boundary. Its
	// memory journal, like all of its records, ends with this process.
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
			p, err := Project(ctx, pgPool, t)
			if err != nil { // a copy for outside tools: the host serves without it
				log.Printf("projection %s: %v", t.ID, err)
				continue
			}
			log.Printf("projected %s into schema %s (reader role %s)", t.ID, ProjectionSchema(t.ID), ReaderRole(t.ID))
			d.runProjection(registry, t, p)
		}
	}
	unregister := observeFrom(registry.list)
	_ = resources.Track("metrics callback", func(context.Context) error { return unregister() })
	_ = resources.Track("background work", runWorkFrom(registry.list))
	var snapshots *snapshotter
	watch := func(original *Tenant) {
		if snapshots == nil {
			return
		}
		snapshots.mu.Lock()
		snapshots.saved[original.ID] = restored[original.ID] // what was replayed since is worth saving too
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
					snapshots.save(ctx, registry.current(original.ID), false)
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
	host.tenantsFrom = registry.list
	host.Templates = d.Templates
	if d.Rebuild != nil { // the host console creates tenants
		var createMu sync.Mutex
		host.CreateTenant = func(req CreateTenantRequest, by string) (*Tenant, error) {
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
			registry.add(t)
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
		// The host serves its seats as signed tokens, and takes only those: the
		// delivery path's development tokens are not accepted here.
		host.SignWith(signer, d.TokenTTL)
	}
	return d.listen(ctx, host, func(shutdown context.Context) error {
		if err := resources.Close(shutdown); err != nil {
			return err
		}
		cancel()
		if snapshots != nil {
			for _, t := range registry.list() {
				snapshots.save(shutdown, t, true)
			}
		}
		return nil
	})
}

// listen serves host until the process is told to stop or the listener fails;
// requests in flight finish, then atExit runs (the final snapshots).
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

// createTenant records a new tenant's spec, builds it, opens its journal and
// files, seeds it and applies the template's first decisions.
func (d *Deployment) createTenant(req CreateTenantRequest, by string, files platform.FileStore, open, seed func(*Tenant) error) (*Tenant, error) {
	now := Now()
	spec, tpl, err := d.spec(req, by, now)
	if err != nil {
		return nil, err
	}
	if d.specs == nil {
		d.specs = &specs{file: d.TenantsFile}
	}
	if err := d.specs.add(spec); err != nil {
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

// roundSize is the items a tenant takes in one round before the next tenant's turn.
const roundSize = 10

// Schedule runs the tenants' due work in rounds, each tenant up to size items
// in turn, until none has ready work or the time is up: a tenant
// with a burst of work waits for the others' turns, and none starves.
func Schedule(tenants []*Tenant, now time.Time, size int, within time.Duration) {
	started := time.Now()
	for more := true; more && time.Since(started) < within; {
		more = false
		for _, t := range tenants {
			if t.Round(now, size) {
				more = true
			}
		}
	}
}

// snapshotter saves after the configured growth and a tenth of the previous
// position, and at shutdown when anything changed. SQL snapshots capture owner
// state and archive records inside PostgreSQL rather than collecting Go rows.
type snapshotter struct {
	journal Journals
	code    string
	every   int64
	mu      sync.Mutex
	saved   map[string]int64
}

func (s *snapshotter) save(ctx context.Context, t *Tenant, final bool) {
	if t.quarantined() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	grown := s.journal.Position(t.ID) - s.saved[t.ID]
	if grown == 0 || !final && (grown < s.every || grown < s.saved[t.ID]/10) {
		return
	}
	started := time.Now()
	state, seq, err := t.snapshotRuntime(ctx, s.journal)
	if t.quarantined() {
		return
	}
	if err == nil {
		var key string
		key, err = t.snapshotStorageKey(s.code, state)
		if err == nil {
			err = saveRuntimeSnapshot(ctx, s.journal, t.ID, seq, key, state)
		}
	}
	if err != nil {
		log.Printf("snapshot %s: %v", t.ID, err)
		return
	}
	s.saved[t.ID] = seq
	log.Printf("saved a snapshot of %s at %d (%d kB) in %v", t.ID, seq, len(state)/1024, time.Since(started).Round(time.Millisecond))
}

// CodeOf identifies the binary and its app versions. It is diagnostic metadata
// for versioned checkpoints and the eligibility key for legacy snapshots or
// owners that have not declared their snapshot restore format.
func CodeOf(tenants ...*Tenant) string {
	h := sha256.New()
	if exe, err := os.Executable(); err == nil {
		if f, err := os.Open(exe); err == nil {
			io.Copy(h, f)
			f.Close()
		}
	}
	for _, t := range tenants {
		for _, a := range t.apps {
			fmt.Fprintf(h, "%s %s@%s\n", t.ID, a.Manifest().ID, a.Manifest().Version)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Work notifications coalesce; the timer still finds delayed or missed work.
// Both use the same scheduler and outside lane, not a second execution path.
var workReady = make(chan struct{}, 1)

func wakeWork() {
	select {
	case workReady <- struct{}{}:
	default:
	}
}

// RunWork runs the tenants' owned work and sends their outbound
// effects when changed and at least every second.
func RunWork(tenants ...*Tenant) func(context.Context) error {
	return runWorkFrom(func() []*Tenant { return tenants })
}

func runWorkFrom(current func() []*Tenant) func(context.Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	var loops, outsideRuns sync.WaitGroup
	loops.Add(2)
	go func() {
		defer loops.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-workReady:
			case <-ticker.C:
			}
			if ctx.Err() != nil {
				return
			}
			tenants := current()
			Schedule(tenants, Now(), roundSize, 900*time.Millisecond)
			// Work on the outside — effects and agents' model calls
			// — goes to the I/O lane of every tenant at once, outside
			// their locks; the next tick does not wait for it.
			var outside []func()
			for _, t := range tenants {
				outside = append(append(outside, t.dispatches(Now())...), t.turns(Now())...)
				outside = append(outside, t.continuousSources(Now())...)
			}
			outsideRuns.Add(1)
			go func() { defer outsideRuns.Done(); integrations.OnLane(outside) }()
		}
	}()
	go func() { // evaluations, embeddings and old transcripts: many calls, apart from runs
		defer loops.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			for _, t := range current() {
				if t.businessStopped() {
					continue
				}
				t.Evaluate(Now())
				t.PullSources(Now())
				t.CheckConnections(Now())
				t.RunPipelines(Now())
				t.Embed(Now())
				t.PurgeTranscripts(Now())
				t.SweepUploads(Now())
			}
		}
	}()
	done := make(chan struct{})
	var stop sync.Once
	return func(ctx context.Context) error {
		stop.Do(func() { cancel(); go func() { loops.Wait(); outsideRuns.Wait(); close(done) }() })
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Host authority is a deployment choice, independent of tenant membership.
func (d *Deployment) hostAdministrators() map[string]bool {
	out := map[string]bool{}
	for _, subject := range strings.Split(d.HostAdmins, ",") {
		if subject = strings.TrimSpace(subject); subject != "" {
			out[subject] = true
		}
	}
	return out
}

// OIDC verifies identities with a public key; personal tokens need a separate,
// durable private secret. An external issuer must never use the development key.
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

// preparePackageIndex gives every tenant the offered deployment catalogue;
// installed package state remains in each tenant's accepted Console state.
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
	if console, ok := tenant.app(controlowner.PlatformApp).(packages.Catalog); ok && d.packageIndex != nil {
		console.BindPackageIndex(d.packageIndex)
	}
}

// Recovery and host-created tenants receive the same offered catalogue.
func (d *Deployment) rebuildTenant(id string) (*Tenant, error) {
	return d.composeRuntime(id, DefaultRuntimeCapabilities().Selection())
}
