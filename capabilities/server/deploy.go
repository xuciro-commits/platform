package platformserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Deployment is how a host binary runs (ADR-0007, ADR-0010): in memory with
// development tokens, or with a PostgreSQL journal and an OpenID provider.
type Deployment struct {
	Addr, Database, Issuer, Keys, Directory, Web string
	Files                                        string // http(s)://host:port/bucket of an S3-compatible store (ADR-0028); empty: memory
	Project                                      bool
	SnapshotEvery                                int64
	// Profile is which infrastructure the host runs on (ADR-0049 D1):
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
	// Packages is a directory of package descriptors (ADR-0047 §10.3): the
	// index the console lists and prechecks. Empty: no packages offered.
	Packages string
	// Rebuild composes a fresh, unstarted tenant with the same durable app and
	// connector declarations. Required for an in-process recovery retry.
	Rebuild func(id string) (*Tenant, error)
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
	flag.StringVar(&d.Keys, "oidc-keys", "", "JWKS URL of the issuer, when the server reaches it on another address")
	flag.StringVar(&d.Files, "files", "", "S3-compatible store of file bytes, http(s)://host:port/bucket, keys from PLATFORM_S3_ACCESS_KEY and PLATFORM_S3_SECRET_KEY (empty: memory)")
	flag.StringVar(&d.Directory, "directory", "", "JSON file with the seats of every tenant (empty: the built-in development seats)")
	flag.BoolVar(&d.Project, "project", false, "copy records into PostgreSQL tables per tenant for tools outside the host (ADR-0019; needs -database)")
	flag.Int64Var(&d.SnapshotEvery, "snapshot-every", 10000, "save each tenant's state every this many journal entries and at shutdown, and start from the newest snapshot of this code (ADR-0019; 0: replay the whole journal)")
	flag.StringVar(&d.Web, "web", "", "directory of the workspace's build, served at / (ADR-0018; empty: API only)")
	flag.StringVar(&d.Profile, "profile", "delivery", "delivery: PostgreSQL journal, S3 file bytes, external OpenID provider; lightweight: one -data directory, an internal signing key, no external services")
	flag.StringVar(&d.Data, "data", "", "directory of a lightweight host's state, journals and file bytes (ADR-0049; required with -profile lightweight)")
	flag.StringVar(&d.IDPKey, "idp-key", "", "file with the lightweight host's token signing key (default: <data>/idp.key)")
	flag.BoolVar(&d.NewKey, "idp-new-key", false, "make a signing key when none exists, refuse to replace one, and stop")
	flag.StringVar(&d.Mint, "mint-token", "", "print an access token for this subject and stop (<user:email> or <client:id>, as the seats name them)")
	flag.DurationVar(&d.TokenTTL, "token-ttl", 12*time.Hour, "how long a minted token is accepted")
	flag.StringVar(&d.Packages, "packages", "", "directory of package descriptors the console offers (empty: no packages)")
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
// refused rather than ignored (ADR-0049 D1).
func (d *Deployment) validate() error {
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
			"-oidc-keys": d.Keys != "", "-project": d.Project} {
			if set {
				return fmt.Errorf("%s is not part of the lightweight profile: its state is the -data directory (ADR-0049 D1)", name)
			}
		}
		return nil
	}
	return fmt.Errorf("unknown -profile %q: delivery, lightweight", d.Profile)
}

// idp, on a lightweight host, is the provider it signs and verifies tokens
// with. The seats served on /v1/sign-in are signed too, so no token of an
// unsigned form is ever accepted (ADR-0049 D3).
func (d *Deployment) lightweightIdP() (*LocalIdP, error) {
	key, err := LoadIDPKey(d.idpKeyPath())
	if err != nil {
		return nil, err
	}
	return NewLocalIdP(key)
}

// makeKey writes a fresh signing key, refusing to replace one that exists: a
// replaced key would sign out every token already minted.
func (d *Deployment) makeKey() error {
	if _, err := os.Stat(d.idpKeyPath()); err == nil {
		return fmt.Errorf("idp: %s already exists; it is not replaced", d.idpKeyPath())
	}
	key, err := NewIDPKey()
	if err != nil {
		return err
	}
	return SaveIDPKey(d.idpKeyPath(), key)
}

// lightweightState opens what a lightweight host runs on: its journal, its
// file bytes and its signing key, all inside -data (ADR-0049 D2-D4).
func (d *Deployment) lightweightState() (Journals, FileStore, *LocalIdP, error) {
	journal, err := OpenFileJournal(filepath.Join(d.Data, "journal"))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("journal: %w", err)
	}
	files, err := NewLocalFiles(filepath.Join(d.Data, "files"))
	if err != nil {
		journal.Close()
		return nil, nil, nil, err
	}
	idp, err := d.lightweightIdP()
	if err != nil {
		journal.Close()
		return nil, nil, nil, err
	}
	return journal, files, idp, nil
}

// Seats reads the directory file, or returns the development seats.
func (d *Deployment) Seats(development []Seat) []Seat {
	if d.Directory == "" {
		return development
	}
	var seats []Seat
	raw, err := os.ReadFile(d.Directory)
	if err == nil {
		err = json.Unmarshal(raw, &seats)
	}
	if err != nil {
		log.Fatalf("directory: %v", err)
	}
	return seats
}

// restoreTenants brings every tenant to the journal's state before the host
// serves: the newest snapshot of this code when there is one, then the entries
// after it. Both profiles run this same path (ADR-0049 D6): the lightweight
// profile's journal is a file, not a second set of semantics, so a restart
// replays and a damaged journal quarantines exactly as it does on PostgreSQL.
func (d *Deployment) restoreTenants(ctx context.Context, journal Journals, code string, tenants []*Tenant, restored map[string]int64, fresh map[string]bool) error {
	for _, t := range tenants {
		var after int64
		if d.SnapshotEvery > 0 {
			seq, state, ok, err := journal.Snapshot(ctx, t.ID, code)
			if err != nil {
				if errors.Is(err, errTenantSnapshot) {
					t.quarantine(err)
					log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
					continue
				}
				return fmt.Errorf("snapshot %s: %w", t.ID, err)
			}
			if ok {
				if err := t.recoverSnapshot(state, seq); err != nil {
					log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
					continue
				}
				after = seq
			}
			restored[t.ID] = after
		}
		entries, err := journal.Entries(ctx, t.ID, after)
		if err != nil {
			if errors.Is(err, errTenantJournal) {
				t.quarantine(err)
				log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
				continue
			}
			return fmt.Errorf("read journal %s: %w", t.ID, err) // a database error is not tenant-local
		}
		if err := t.recoverEntries(entries); err != nil {
			log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
			continue
		}
		fresh[t.ID] = after == 0 && len(entries) == 0
		if t.procs != nil {
			if err := t.procs.Check(); err != nil { // running instances need their flow's version (ADR-0020 D6)
				t.quarantine(fmt.Errorf("process recovery: %w", err))
				log.Printf("quarantined %s: %s", t.ID, t.fault.Load().Reason)
				continue
			}
		}
		if after > 0 {
			log.Printf("restored %s from the snapshot at %d, then replayed %d entries", t.ID, after, len(entries))
		} else {
			log.Printf("replayed %d entries for %s", len(entries), t.ID)
		}
		// An input the journal did not take is never answered; the client's
		// outbox resends it after the restart has replayed the rest.
		t.attachJournal(ctx, journal)
	}
	return nil
}

// Serve replays each tenant's journal, from its newest snapshot of this code
// when there is one, then records into it (fail-stop), seeds a tenant whose
// journal was empty, runs the tenants' owned
// work every second (ADR-0013), saves snapshots as the journal grows and at
// shutdown (ADR-0019), and serves until SIGINT or SIGTERM.
func (d *Deployment) Serve(tenants ...*Tenant) error {
	ctx := context.Background()
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
	var idp *LocalIdP
	if d.Profile == "lightweight" {
		var err error
		if idp, err = d.lightweightIdP(); err != nil {
			return err
		}
	}
	if d.Mint != "" {
		if idp == nil {
			return fmt.Errorf("-mint-token is the lightweight profile's; a delivery host signs in at its OpenID provider")
		}
		token, err := idp.Mint(d.Mint, d.TokenTTL, Now())
		if err != nil {
			return err
		}
		fmt.Println(token)
		return nil
	}
	registry := newTenantRegistry(tenants)
	flush := exportTelemetry(ctx, filepath.Base(os.Args[0])) // traces and metrics, when an OTLP endpoint is set (ADR-0027 D5)
	defer flush(context.Background())
	var lightweightFiles FileStore // the lightweight profile's bytes open with its journal, all in -data
	switch {
	case d.Files != "":
		store, err := NewS3Files(ctx, d.Files)
		if err != nil {
			return err
		}
		for _, t := range tenants {
			t.Files = store
		}
		log.Printf("files in %s", d.Files)
	}
	var journal Journals
	code := CodeOf(tenants...)
	restored := map[string]int64{} // each tenant's snapshot position at start-up, 0 without one
	fresh := map[string]bool{}     // tenants whose journal was empty
	switch {
	case d.Profile == "lightweight":
		var err error
		journal, lightweightFiles, idp, err = d.lightweightState()
		if err != nil {
			return err
		}
		defer journal.Close()
		for _, t := range tenants {
			t.Files = lightweightFiles
		}
		log.Printf("journal and file bytes in %s", d.Data)
	case d.Database != "":
		var err error
		if journal, err = OpenJournal(ctx, d.Database); err != nil {
			return fmt.Errorf("journal: %w", err)
		}
	}
	if journal != nil {
		if err := d.restoreTenants(ctx, journal, code, tenants, restored, fresh); err != nil {
			return err
		}
	}
	if journal == nil {
		// The development host still uses the accepted-result boundary. Its
		// memory journal, like all of its records, ends with this process.
		for _, tenant := range tenants {
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
			if tenant.AcceptResult == nil {
				tenant.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
					appendEntry(e)
					return e.Body, nil
				}
			}
		}
	}
	for _, t := range tenants {
		if d.Seed != nil && !t.quarantined() && (journal == nil || fresh[t.ID]) {
			if err := d.Seed(t, time.Now()); err != nil {
				return fmt.Errorf("seed %s: %w", t.ID, err)
			}
			log.Printf("seeded %s", t.ID)
		}
	}
	authenticate := Authenticate(func(token string) (string, bool) { return token, token != "" })
	if d.Issuer != "" {
		if d.Keys == "" {
			return fmt.Errorf("-oidc-keys is required with -oidc-issuer")
		}
		authenticate = OIDC(d.Issuer, d.Keys)
	}
	if pgPool, pg := pool(journal); journal != nil && d.Project && pg {
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
			flushProjection(ctx, registry, t, p)
		}
	}
	runWorkFrom(registry.list)
	observeFrom(registry.list)
	var snapshots *snapshotter
	if journal != nil && d.SnapshotEvery > 0 {
		snapshots = &snapshotter{journal: journal, code: code, every: d.SnapshotEvery, saved: map[string]int64{}}
		for _, original := range tenants {
			snapshots.saved[original.ID] = restored[original.ID] // what was replayed since is worth saving too
			go func() {
				for range time.Tick(5 * time.Second) {
					snapshots.save(ctx, registry.current(original.ID), false)
				}
			}()
		}
	}
	host := NewHost(authenticate, tenants...)
	host.tenantsFrom = registry.list
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
	if idp != nil {
		// The host serves its seats as signed tokens, and takes only those: the
		// delivery path's development tokens are not accepted here (ADR-0049 D3).
		host.SignWith(idp, d.TokenTTL)
	}
	server := &http.Server{Addr: d.Addr, Handler: host.Handler()}
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
	server.Shutdown(shutdown) // requests in flight finish; no new ones
	if snapshots != nil {
		for _, t := range registry.list() {
			snapshots.save(ctx, t, true)
		}
	}
	return nil
}

// roundSize is the items a tenant takes in one round before the next tenant's turn.
const roundSize = 10

// Schedule runs the tenants' due work in rounds, each tenant up to size items
// in turn (ADR-0027 D3), until none has ready work or the time is up: a tenant
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

// snapshotter saves a tenant's snapshot once the journal has grown by every
// entries and by a tenth since the last one (decisions wait while a snapshot
// captures the state, about a second at a million records, so a large tenant
// takes them rarely), and at shutdown when it grew at all.
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
	state, seq, err := t.Snapshot(func() int64 { return s.journal.Position(t.ID) })
	if t.quarantined() {
		return
	}
	if err == nil {
		err = s.journal.SaveSnapshot(ctx, t.ID, seq, s.code, state)
	}
	if err != nil {
		log.Printf("snapshot %s: %v", t.ID, err)
		return
	}
	s.saved[t.ID] = seq
	log.Printf("saved a snapshot of %s at %d (%d kB) in %v", t.ID, seq, len(state)/1024, time.Since(started).Round(time.Millisecond))
}

// CodeOf names the code a snapshot is valid for: this binary and the versions
// of its apps. Other code replays the whole journal, as apps evolve by
// replaying through new code (ADR-0007), then saves its own snapshot.
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

// RunWork runs the tenants' owned work (ADR-0013) and sends their outbound
// effects (ADR-0014) every second, for as long as the process lives.
func RunWork(tenants ...*Tenant) {
	runWorkFrom(func() []*Tenant { return tenants })
}

func runWorkFrom(current func() []*Tenant) {
	go func() {
		for range time.Tick(time.Second) {
			tenants := current()
			Schedule(tenants, Now(), roundSize, 900*time.Millisecond)
			// Work on the outside — effects (ADR-0014) and agents' model calls
			// (ADR-0021) — goes to the I/O lane of every tenant at once, outside
			// their locks; the next tick does not wait for it (ADR-0027 D2).
			var outside []func()
			for _, t := range tenants {
				outside = append(append(outside, t.dispatches(Now())...), t.turns(Now())...)
			}
			go onLane(outside)
		}
	}()
	go func() { // evaluations, embeddings and old transcripts: many calls, apart from runs
		for range time.Tick(5 * time.Second) {
			for _, t := range current() {
				if t.quarantined() {
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
}
