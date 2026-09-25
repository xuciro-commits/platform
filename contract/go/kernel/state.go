package kernel

import (
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// Saving and restoring what the Go logs hold, for a host's snapshot at a
// journal position (docs/ADR/0019 D6). This adds no semantics: a restored log
// answers exactly as the log it was saved from, and the host checks that a
// snapshot plus the entries after it equals a full replay.

// Restore loads a tenant's records into an empty log, as they were recorded.
func (l *ChangeLog) Restore(tenant string, records []*pb.ChangeRecord) {
	l.logs[tenant] = slices.Clone(records)
	l.byKey[tenant] = map[string]*pb.ChangeRecord{}
	for _, r := range records {
		l.byKey[tenant][r.GetSubmission().GetIdempotencyKey()] = r
		l.revs[[3]string{tenant, r.GetSubmission().GetTarget().GetType(), r.GetSubmission().GetTarget().GetId()}]++
	}
	l.next += len(records) // change IDs continue after the restored ones
}

// Restore loads a tenant's facts into an empty log, as they were recorded.
func (l *FactLog) Restore(tenant string, records []*pb.FactRecord) {
	l.logs[tenant] = slices.Clone(records)
	l.byKey[tenant] = map[string]*pb.FactRecord{}
	for _, r := range records {
		l.byKey[tenant][r.GetFact().GetIdempotencyKey()] = r
	}
	l.next += len(records)
}

// IdentityState is what an Identity knows: its entities and their redirects.
type IdentityState struct {
	Entities  []Ref           `json:"entities"`
	Redirects []RedirectState `json:"redirects"`
}

type RedirectState struct {
	From Ref   `json:"from"`
	To   []Ref `json:"to"`
}

func sortRefs(a, b Ref) int { return strings.Compare(a.Type+"/"+a.ID, b.Type+"/"+b.ID) }

func (id *Identity) State() IdentityState {
	s := IdentityState{Entities: []Ref{}, Redirects: []RedirectState{}}
	for r := range id.entities {
		s.Entities = append(s.Entities, r)
	}
	for from, to := range id.redirects {
		s.Redirects = append(s.Redirects, RedirectState{From: from, To: to})
	}
	slices.SortFunc(s.Entities, sortRefs)
	slices.SortFunc(s.Redirects, func(a, b RedirectState) int { return sortRefs(a.From, b.From) })
	return s
}

func (id *Identity) Restore(s IdentityState) {
	id.entities, id.redirects = map[Ref]bool{}, map[Ref][]Ref{}
	for _, r := range s.Entities {
		id.entities[r] = true
	}
	for _, r := range s.Redirects {
		id.redirects[r.From] = r.To
	}
}

// All are the works, by ID.
func (w *Works) All() []*pb.Work {
	out := make([]*pb.Work, 0, len(w.items))
	for _, x := range w.items {
		out = append(out, x)
	}
	slices.SortFunc(out, func(a, b *pb.Work) int { return strings.Compare(a.GetWorkId(), b.GetWorkId()) })
	return out
}

func (w *Works) Restore(items []*pb.Work) {
	w.items = map[string]*pb.Work{}
	for _, x := range items {
		w.items[x.GetWorkId()] = x
	}
}

// ConnectorMark is where a connector is: its cursor and when it was last seen.
type ConnectorMark struct {
	Tenant    string    `json:"tenant"`
	Connector string    `json:"connector"`
	Cursor    string    `json:"cursor,omitempty"`
	LastSeen  time.Time `json:"lastSeen,omitzero"`
}

// State is every registered connector's descriptor and mark.
func (c *Connectors) State() ([]*pb.ConnectorDescriptor, []ConnectorMark) {
	var descriptors []*pb.ConnectorDescriptor
	var marks []ConnectorMark
	for key, d := range c.descriptors {
		descriptors = append(descriptors, d)
		marks = append(marks, ConnectorMark{Tenant: key[0], Connector: key[1], Cursor: c.cursors[key], LastSeen: c.lastSeen[key]})
	}
	slices.SortFunc(descriptors, func(a, b *pb.ConnectorDescriptor) int {
		return strings.Compare(a.GetTenantId()+"/"+a.GetConnectorId(), b.GetTenantId()+"/"+b.GetConnectorId())
	})
	slices.SortFunc(marks, func(a, b ConnectorMark) int {
		return strings.Compare(a.Tenant+"/"+a.Connector, b.Tenant+"/"+b.Connector)
	})
	return descriptors, marks
}

func (c *Connectors) Restore(descriptors []*pb.ConnectorDescriptor, marks []ConnectorMark) {
	c.descriptors, c.cursors, c.lastSeen = map[[2]string]*pb.ConnectorDescriptor{}, map[[2]string]string{}, map[[2]string]time.Time{}
	for _, d := range descriptors {
		c.descriptors[[2]string{d.GetTenantId(), d.GetConnectorId()}] = d
	}
	for _, m := range marks {
		key := [2]string{m.Tenant, m.Connector}
		if m.Cursor != "" {
			c.cursors[key] = m.Cursor
		}
		if !m.LastSeen.IsZero() {
			c.lastSeen[key] = m.LastSeen
		}
	}
}
