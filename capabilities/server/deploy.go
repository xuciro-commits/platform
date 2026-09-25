package platformserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Deployment is how a host binary runs (ADR-0007, ADR-0010): in memory with
// development tokens, or with a PostgreSQL journal and an OpenID provider.
type Deployment struct {
	Addr, Database, Issuer, Keys, Directory, Web string
	Project                                      bool
	SnapshotEvery                                int64
}

// Flags registers the deployment flags on the default flag set.
func Flags(addr string) *Deployment {
	d := &Deployment{}
	flag.StringVar(&d.Addr, "addr", addr, "listen address")
	flag.StringVar(&d.Database, "database", "", "PostgreSQL URL of the journal (empty: memory only)")
	flag.StringVar(&d.Issuer, "oidc-issuer", "", "OpenID issuer whose access tokens are accepted (empty: development tokens, the token is the subject)")
	flag.StringVar(&d.Keys, "oidc-keys", "", "JWKS URL of the issuer, when the server reaches it on another address")
	flag.StringVar(&d.Directory, "directory", "", "JSON file with the seats of every tenant (empty: the built-in development seats)")
	flag.BoolVar(&d.Project, "project", false, "copy records into PostgreSQL tables per tenant for tools outside the host (ADR-0019; needs -database)")
	flag.Int64Var(&d.SnapshotEvery, "snapshot-every", 10000, "save each tenant's state every this many journal entries and at shutdown, and start from the newest snapshot of this code (ADR-0019; 0: replay the whole journal)")
	flag.StringVar(&d.Web, "web", "", "directory of the workspace's build, served at / (ADR-0018; empty: API only)")
	return d
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

// Serve replays each tenant's journal, from its newest snapshot of this code
// when there is one, then records into it (fail-stop), runs the tenants' owned
// work every second (ADR-0013), saves snapshots as the journal grows and at
// shutdown (ADR-0019), and serves until SIGINT or SIGTERM.
func (d *Deployment) Serve(tenants ...*Tenant) error {
	ctx := context.Background()
	var journal *Journal
	code := CodeOf(tenants...)
	restored := map[string]int64{} // each tenant's snapshot position at start-up, 0 without one
	if d.Database != "" {
		var err error
		if journal, err = OpenJournal(ctx, d.Database); err != nil {
			return fmt.Errorf("journal: %w", err)
		}
		for _, t := range tenants {
			var after int64
			if d.SnapshotEvery > 0 {
				seq, state, ok, err := journal.Snapshot(ctx, t.ID, code)
				if err != nil {
					return fmt.Errorf("snapshot %s: %w", t.ID, err)
				}
				if ok {
					if err := t.Restore(state); err != nil {
						return fmt.Errorf("restore %s from the snapshot at %d: %w (start with -snapshot-every=0 to replay the whole journal)", t.ID, seq, err)
					}
					after = seq
				}
				restored[t.ID] = after
			}
			entries, err := journal.Entries(ctx, t.ID, after)
			if err == nil {
				err = t.Replay(entries)
			}
			if err != nil {
				return fmt.Errorf("replay %s: %w", t.ID, err)
			}
			if after > 0 {
				log.Printf("restored %s from the snapshot at %d, then replayed %d entries", t.ID, after, len(entries))
			} else {
				log.Printf("replayed %d entries for %s", len(entries), t.ID)
			}
			// An input the journal did not take is never answered; the client's
			// outbox resends it after the restart has replayed the rest.
			t.Record = func(e Entry) {
				if err := journal.Append(ctx, t.ID, e); err != nil {
					log.Fatalf("journal append: %v", err)
				}
			}
		}
	}
	authenticate := Authenticate(func(token string) (string, bool) { return token, token != "" })
	if d.Issuer != "" {
		if d.Keys == "" {
			return fmt.Errorf("-oidc-keys is required with -oidc-issuer")
		}
		authenticate = OIDC(d.Issuer, d.Keys)
	}
	if journal != nil && d.Project {
		for _, t := range tenants {
			p, err := Project(ctx, journal.pool, t)
			if err != nil { // a copy for outside tools: the host serves without it
				log.Printf("projection %s: %v", t.ID, err)
				continue
			}
			log.Printf("projected %s into schema %s (reader role %s)", t.ID, ProjectionSchema(t.ID), ReaderRole(t.ID))
			go func() {
				for range time.Tick(time.Second) {
					if err := p.Flush(ctx); err != nil {
						log.Printf("projection %s: %v", t.ID, err)
					}
				}
			}()
		}
	}
	RunWork(tenants...)
	var snapshots *snapshotter
	if journal != nil && d.SnapshotEvery > 0 {
		snapshots = &snapshotter{journal: journal, code: code, every: d.SnapshotEvery, saved: map[string]int64{}}
		for _, t := range tenants {
			snapshots.saved[t.ID] = restored[t.ID] // what was replayed since is worth saving too
			go func() {
				for range time.Tick(5 * time.Second) {
					snapshots.save(ctx, t, false)
				}
			}()
		}
	}
	host := NewHost(authenticate, tenants...)
	host.Web, host.Issuer, host.Client, host.Development = d.Web, d.Issuer, "platform-web", d.Issuer == ""
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
		for _, t := range tenants {
			snapshots.save(ctx, t, true)
		}
	}
	return nil
}

// snapshotter saves a tenant's snapshot once the journal has grown by every
// entries and by a tenth since the last one (decisions wait while a snapshot
// captures the state, about a second at a million records, so a large tenant
// takes them rarely), and at shutdown when it grew at all.
type snapshotter struct {
	journal *Journal
	code    string
	every   int64
	mu      sync.Mutex
	saved   map[string]int64
}

func (s *snapshotter) save(ctx context.Context, t *Tenant, final bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	grown := s.journal.Position(t.ID) - s.saved[t.ID]
	if grown == 0 || !final && (grown < s.every || grown < s.saved[t.ID]/10) {
		return
	}
	started := time.Now()
	state, seq, err := t.Snapshot(func() int64 { return s.journal.Position(t.ID) })
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
	go func() {
		for range time.Tick(time.Second) {
			for _, t := range tenants {
				t.Work(Now())
				t.Dispatch(Now()) // outbound effects, outside the tenant's lock (ADR-0014)
			}
		}
	}()
}
