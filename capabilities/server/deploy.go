package platformserver

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

// Deployment is how a host binary runs (ADR-0007, ADR-0010): in memory with
// development tokens, or with a PostgreSQL journal and an OpenID provider.
type Deployment struct {
	Addr, Database, Issuer, Keys, Directory string
}

// Flags registers the deployment flags on the default flag set.
func Flags(addr string) *Deployment {
	d := &Deployment{}
	flag.StringVar(&d.Addr, "addr", addr, "listen address")
	flag.StringVar(&d.Database, "database", "", "PostgreSQL URL of the journal (empty: memory only)")
	flag.StringVar(&d.Issuer, "oidc-issuer", "", "OpenID issuer whose access tokens are accepted (empty: development tokens, the token is the subject)")
	flag.StringVar(&d.Keys, "oidc-keys", "", "JWKS URL of the issuer, when the server reaches it on another address")
	flag.StringVar(&d.Directory, "directory", "", "JSON file with the seats of every tenant (empty: the built-in development seats)")
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

// Serve replays each tenant's journal, then records into it (fail-stop) and serves.
func (d *Deployment) Serve(tenants ...*Tenant) error {
	if d.Database != "" {
		ctx := context.Background()
		journal, err := OpenJournal(ctx, d.Database)
		if err != nil {
			return fmt.Errorf("journal: %w", err)
		}
		for _, t := range tenants {
			entries, err := journal.Entries(ctx, t.ID)
			if err == nil {
				err = t.Replay(entries)
			}
			if err != nil {
				return fmt.Errorf("replay %s: %w", t.ID, err)
			}
			log.Printf("replayed %d entries for %s", len(entries), t.ID)
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
	log.Printf("host on http://%s", d.Addr)
	return http.ListenAndServe(d.Addr, NewHost(authenticate, tenants...).Handler())
}
