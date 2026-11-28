package host

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"platformserver/services/objects"
	build "platformserver/services/resources/authoring"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// This is the host boundary, not just a test of the accumulator.
func TestInvariantK47LedgerFactsAndProjection(t *testing.T) {
	live := newInvariantTenant(t, "invariant-ledger")
	const fact = "invariantproject.posting"
	const balance = fact + ".balance"
	live.accept(t, "build.object.create", build.ObjectType, "POSTING", map[string]any{
		"name": "posting", "title": "Posting", "ledger": true,
		"fields":   []map[string]any{{"name": "bucket", "title": "Bucket", "type": "text", "required": true}, {"name": "qty", "title": "Quantity", "type": "integer", "required": true}},
		"balances": []map[string]any{{"name": "balance", "title": "Balance", "keys": []string{"bucket"}, "measures": []string{"qty"}, "constraints": []platform.Constraint{{Name: "available", Kind: "expression", Fields: []string{"qty"}, Expr: &platform.Expr{Op: ">=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("integer", "qty"), {Kind: "integer", Lit: &platform.ExprLiteral{Value: platform.Raw(0)}}}}, Message: "Not enough quantity"}}}},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "POSTING", nil)
	live.accept(t, fact+".create", fact, "A", map[string]any{"bucket": "K", "qty": 10})
	member := live.member(t)
	var wg sync.WaitGroup
	refusals := make(chan *platform.Error, 2)
	for _, id := range []string{"B", "C"} {
		submission := live.submission(fact+".create", fact, id, map[string]any{"bucket": "K", "qty": -6})
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, refusal := live.Submit(member, submission, invariantClock)
			refusals <- refusal
		}()
	}
	wg.Wait()
	close(refusals)
	accepted, rejected := 0, 0
	for refusal := range refusals {
		if refusal == nil {
			accepted++
		} else if strings.Contains(refusal.Message, "Not enough quantity") {
			rejected++
		} else {
			t.Fatalf("wrong refusal: %+v", refusal)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("concurrent 6+6: %d accepted, %d refused", accepted, rejected)
	}
	read := func(l *invariantTenant) json.RawMessage {
		t.Helper()
		page, err := l.Records(t.Context(), l.member(t), balance, platform.Query{}, invariantClock)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Records) != 1 {
			t.Fatalf("balance count: %s", platform.Raw(page))
		}
		return platform.Raw(page.Records[0])
	}
	before := read(live)
	if diff, err := live.Projection.ReconcileLedger(fact); err != nil || len(diff) > 0 {
		t.Fatalf("reconcile: %v %v", diff, err)
	}
	raw, _, err := live.Snapshot(func() int64 { return int64(len(live.entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := newInvariantTenantReplayWithApps(t, live.ID, nil)
	if err := restored.Restore(raw); err != nil {
		t.Fatalf("snapshot restore: %v", err)
	}
	if after := read(restored); string(after) != string(before) {
		t.Fatalf("snapshot balance differs: %s != %s", after, before)
	}
	period, refusal := live.AggregateObjectSet(live.member(t), objects.ObjectSetRequest{Set: platform.ObjectSet{Object: balance}, Groups: []string{"bucket"}, Measures: []string{"sum:qty"}, From: "2026-10-01", To: "2026-10-31"}, invariantClock)
	if refusal != nil || len(period.Rows) != 1 {
		t.Fatalf("ledger period: %+v %v", period, refusal)
	}
	for k, want := range map[string]string{"opening:qty": "0", "debit:qty": "10", "credit:qty": "6", "closing:qty": "4"} {
		if string(platform.Raw(period.Rows[0][k])) != want {
			t.Fatalf("period %s: %v want %s", k, period.Rows[0][k], want)
		}
	}
	var row map[string]any
	if json.Unmarshal(before, &row) != nil || row["qty"] != float64(4) {
		t.Fatalf("balance: %s", before)
	}
	info, ok := live.Entity(fact)
	if !ok {
		t.Fatal("ledger type missing")
	}
	for _, schema := range info.Standard {
		if schema == fact+".edit" || schema == fact+".archive" {
			t.Fatalf("mutable ledger action declared: %s", schema)
		}
	}
	for id, row := range live.Projection.Type(fact).Rows().All() {
		et := live.Projection.Type(fact)
		changed := *row
		changed.Value = records.Copy(et.Info().Go, row.Value.Interface())
		records.Header(changed.Value).Archived = true
		if err := et.CheckLedgerRow(changed.Value, row); err == nil {
			t.Fatal("ledger preflight allowed editing an accepted fact")
		}
		if err := et.SetRow(id, &changed); err == nil {
			t.Fatal("ledger store allowed editing an accepted fact")
		}
		break
	}
	et := live.Projection.Type(balance)
	for id, row := range et.Rows().All() {
		copy := *row
		copy.Value = records.Copy(et.Info().Go, row.Value.Interface())
		if err := et.SetRow(id, &copy); err == nil {
			t.Fatal("second balance writer allowed")
		}
	}
	for _, schema := range []string{fact + ".edit", fact + ".archive", balance + ".create", balance + ".edit", balance + ".archive"} {
		typ := fact
		if strings.HasPrefix(schema, balance+".") {
			typ = balance
		}
		if _, refusal := live.Submit(live.member(t), live.submission(schema, typ, "A", map[string]any{}), invariantClock); refusal == nil {
			t.Fatalf("immutable record accepted %s", schema)
		}
	}
	fresh := live.restore(t, live.entries)
	if after := read(fresh); string(after) != string(before) {
		t.Fatalf("replay balance differs: %s != %s", after, before)
	}
}

func TestInvariantK48LedgerReversalAndClose(t *testing.T) {
	live := newInvariantTenant(t, "invariant-ledger-reverse")
	const fact = "invariantproject.posting"
	const balance = fact + ".balance"
	live.accept(t, "build.object.create", build.ObjectType, "POSTING", map[string]any{
		"name": "posting", "title": "Posting", "ledger": true, "closedBefore": "2026-10-01",
		"fields":   []map[string]any{{"name": "bucket", "title": "Bucket", "type": "text", "required": true}, {"name": "qty", "title": "Quantity", "type": "integer", "required": true}},
		"balances": []map[string]any{{"name": "balance", "title": "Balance", "keys": []string{"bucket"}, "measures": []string{"qty"}}},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "POSTING", nil)
	live.accept(t, fact+".create", fact, "A", map[string]any{"bucket": "K", "qty": 10, "on": "2026-10-01"})
	live.accept(t, fact+".create", fact, "B", map[string]any{"bucket": "K", "qty": -6})
	before := live.recordImage(t)[fact]
	refuse := func(schema, id string, payload any, reason string) {
		t.Helper()
		_, err := live.Submit(live.member(t), live.submission(schema, fact, id, payload), invariantClock)
		if err == nil || reason != "" && !strings.Contains(err.Message, reason) {
			t.Fatalf("%s %s refusal: %+v", schema, id, err)
		}
	}
	refuse(fact+".create", "C", map[string]any{"bucket": "K", "qty": 1, "on": "2026-09-30"}, "closed")
	refuse(fact+".reverse", "B", map[string]any{"on": "2026-09-30"}, "closed")
	live.accept(t, fact+".reverse", fact, "B", map[string]any{})
	refuse(fact+".reverse", "B", map[string]any{}, "already reversed")
	original, err := live.RecordOf(t.Context(), live.member(t), fact, "B", invariantClock)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if json.Unmarshal(platform.Raw(original.Record), &value) != nil || value["reversed"] != true || value["qty"] != float64(-6) {
		t.Fatalf("reversed marker: %s", platform.Raw(original.Record))
	}
	page, err := live.Records(t.Context(), live.member(t), fact, platform.Query{}, invariantClock)
	if err != nil || len(page.Records) != 3 {
		t.Fatalf("fact count: %+v %v", page, err)
	}
	var reversal bool
	for _, raw := range page.Records {
		var v map[string]any
		_ = json.Unmarshal(platform.Raw(raw), &v)
		if v["reverses"] == "B" && v["qty"] == float64(6) {
			reversal = true
		}
	}
	if !reversal {
		t.Fatal("missing opposite fact")
	}
	var oldRows, newRows []struct{ Value json.RawMessage }
	_ = json.Unmarshal(before, &oldRows)
	_ = json.Unmarshal(live.recordImage(t)[fact], &newRows)
	for _, old := range oldRows {
		found := false
		for _, next := range newRows {
			if string(old.Value) == string(next.Value) {
				found = true
			}
		}
		if !found {
			t.Fatalf("original fact was rewritten: %s", old.Value)
		}
	}
	read := func(l *invariantTenant) json.RawMessage {
		t.Helper()
		page, err := l.Records(t.Context(), l.member(t), balance, platform.Query{}, invariantClock)
		if err != nil || len(page.Records) != 1 {
			t.Fatalf("balance page: %+v %v", page, err)
		}
		return platform.Raw(page.Records[0])
	}
	row := read(live)
	if !strings.Contains(string(row), `"qty":10`) {
		t.Fatalf("reversal balance: %s", row)
	}
	fresh := live.restore(t, live.entries)
	if string(read(fresh)) != string(row) {
		t.Fatal("reversal replay differs")
	}
}

func TestInvariantK48LedgerRecordClose(t *testing.T) {
	live := newInvariantTenant(t, "invariant-ledger-record-close")
	const period = "invariantproject.period"
	const fact = "invariantproject.posting"
	const balance = fact + ".balance"
	live.accept(t, "build.object.create", build.ObjectType, "PERIOD", map[string]any{
		"name": "period", "title": "Period",
		"fields": []map[string]any{{"name": "cutoff", "title": "Cutoff", "type": "date", "required": true}},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "PERIOD", nil)
	live.accept(t, "build.object.create", build.ObjectType, "POSTING", map[string]any{
		"name": "posting", "title": "Posting", "ledger": true, "closedBefore": "2026-09-01",
		"closedBeforeRecord": map[string]any{"type": period, "id": "CURRENT", "field": "cutoff"},
		"fields":             []map[string]any{{"name": "bucket", "title": "Bucket", "type": "text", "required": true}, {"name": "qty", "title": "Quantity", "type": "integer", "required": true}},
		"balances":           []map[string]any{{"name": "balance", "title": "Balance", "keys": []string{"bucket"}, "measures": []string{"qty"}}},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "POSTING", nil)
	reject := func(l *invariantTenant, schema, id string, payload map[string]any, reason string) {
		t.Helper()
		_, err := l.Submit(l.member(t), l.submission(schema, fact, id, payload), invariantClock)
		if err == nil || !strings.Contains(err.Message, reason) {
			t.Fatalf("dynamic close %s %s refusal: %+v, want %s", schema, id, err, reason)
		}
	}
	post := func(day string) map[string]any { return map[string]any{"bucket": "K", "qty": 1, "on": day} }
	reject(live, fact+".create", "MISSING", post("2026-10-01"), "close source unavailable")
	live.accept(t, period+".create", period, "CURRENT", map[string]any{"cutoff": "2026-09-01"})
	live.accept(t, fact+".create", fact, "A", post("2026-09-30"))
	live.accept(t, period+".edit", period, "CURRENT", map[string]any{"cutoff": "2026-10-01"})
	reject(live, fact+".create", "B", post("2026-09-30"), "closed before 2026-10-01")
	reject(live, fact+".reverse", "A", map[string]any{"on": "2026-09-30"}, "closed before 2026-10-01")
	live.accept(t, fact+".reverse", fact, "A", map[string]any{"on": "2026-10-01"})
	live.accept(t, fact+".create", fact, "C", post("2026-10-01"))
	read := func(l *invariantTenant) string {
		t.Helper()
		page, err := l.Records(t.Context(), l.member(t), balance, platform.Query{}, invariantClock)
		if err != nil || len(page.Records) != 1 {
			t.Fatalf("dynamic close balance: %+v %v", page, err)
		}
		return string(platform.Raw(page.Records[0]))
	}
	before := read(live)
	if !strings.Contains(before, `"qty":1`) {
		t.Fatalf("dynamic close balance: %s", before)
	}
	fresh := live.restore(t, live.entries)
	if read(fresh) != before {
		t.Fatal("dynamic close replay changed historical facts")
	}
	reject(fresh, fact+".create", "D", post("2026-09-30"), "closed before 2026-10-01")
	live.accept(t, period+".archive", period, "CURRENT", nil)
	reject(live, fact+".create", "ARCHIVED", post("2026-10-31"), "close source unavailable")
}

type k58StateStore struct {
	persistence.Memory
	mu            sync.RWMutex
	entries       map[string][]persistence.Entry
	seq           map[string]int64
	active        map[string]bool
	records       map[string]map[string]map[string]persistence.StateCurrent
	versions      map[string]map[string]map[string][]persistence.StateCurrent
	changes       map[string]map[string]map[string][]byte
	changeByID    map[string]map[string]map[string][]byte
	changeList    map[string]map[string][][]byte
	values        map[string]map[string]map[string]json.RawMessage
	uniques       map[string]map[string]map[string]map[string]string
	candidateType map[string]*atomic.Int64
	changesCalls  atomic.Int64
	editKeyProbes atomic.Int64
}

func newK58StateStore(trackedTypes ...string) *k58StateStore {
	ct := map[string]*atomic.Int64{}
	for _, typ := range trackedTypes {
		ct[typ] = &atomic.Int64{}
	}
	return &k58StateStore{
		entries: map[string][]persistence.Entry{}, seq: map[string]int64{}, active: map[string]bool{},
		records:       map[string]map[string]map[string]persistence.StateCurrent{},
		versions:      map[string]map[string]map[string][]persistence.StateCurrent{},
		changes:       map[string]map[string]map[string][]byte{},
		changeByID:    map[string]map[string]map[string][]byte{},
		changeList:    map[string]map[string][][]byte{},
		values:        map[string]map[string]map[string]json.RawMessage{},
		uniques:       map[string]map[string]map[string]map[string]string{},
		candidateType: ct,
	}
}
func (m *k58StateStore) Close() {}
func (m *k58StateStore) Position(tenant string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.seq[tenant]
}
func (m *k58StateStore) Entries(_ context.Context, tenant string, after int64) ([]persistence.Entry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if int(after) >= len(m.entries[tenant]) {
		return nil, nil
	}
	return slices.Clone(m.entries[tenant][after:]), nil
}
func (m *k58StateStore) AppendAccepted(_ context.Context, tenant string, e persistence.Entry, _, _ string) (*persistence.AcceptedWrite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq[tenant]++
	pos := m.seq[tenant]
	m.entries[tenant] = append(m.entries[tenant], e)
	var delta *persistence.StateDelta
	if m.active[tenant] && e.Kind == "accepted-result" {
		d, err := storageDelta(e.Body)
		if err != nil {
			return nil, err
		}
		m.applyLocked(tenant, pos, d)
		delta = &d
	}
	return &persistence.AcceptedWrite{Body: slices.Clone(e.Body), Delta: delta}, nil
}
func (m *k58StateStore) AppendAcceptedBatch(_ context.Context, tenant string, batch []persistence.AcceptedAppend) ([][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(batch))
	for i, item := range batch {
		m.seq[tenant]++
		pos := m.seq[tenant]
		m.entries[tenant] = append(m.entries[tenant], item.Entry)
		out[i] = slices.Clone(item.Entry.Body)
		if m.active[tenant] && item.Entry.Kind == "accepted-result" {
			d, err := storageDelta(item.Entry.Body)
			if err != nil {
				return nil, err
			}
			m.applyLocked(tenant, pos, d)
		}
	}
	return out, nil
}
func (m *k58StateStore) SaveSnapshot(context.Context, string, int64, string, []byte) error {
	return nil
}
func (m *k58StateStore) RepairSnapshot(context.Context, string, int64, string, []byte) error {
	return nil
}
func (m *k58StateStore) Snapshot(context.Context, string, string) (int64, []byte, bool, error) {
	return 0, nil, false, nil
}
func (m *k58StateStore) StatePosition(_ context.Context, tenant string) (int64, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.seq[tenant], m.active[tenant], nil
}
func (m *k58StateStore) RestoreState(context.Context, string, int64, persistence.StateCheckpoint, persistence.StateDelta) error {
	return nil
}
func (m *k58StateStore) InitializeState(_ context.Context, tenant string, pos int64, d persistence.StateDelta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active[tenant], m.seq[tenant] = true, pos
	m.applyLocked(tenant, pos, d)
	return nil
}
func (m *k58StateStore) applyLocked(tenant string, pos int64, d persistence.StateDelta) {
	if m.records[tenant] == nil {
		m.records[tenant] = map[string]map[string]persistence.StateCurrent{}
		m.versions[tenant] = map[string]map[string][]persistence.StateCurrent{}
		m.changes[tenant] = map[string]map[string][]byte{}
		m.changeByID[tenant] = map[string]map[string][]byte{}
		m.changeList[tenant] = map[string][][]byte{}
		m.values[tenant] = map[string]map[string]json.RawMessage{}
		m.uniques[tenant] = map[string]map[string]map[string]string{}
	}
	putVersion := func(typ, id string, cur persistence.StateCurrent) {
		if m.records[tenant][typ] == nil {
			m.records[tenant][typ] = map[string]persistence.StateCurrent{}
			m.versions[tenant][typ] = map[string][]persistence.StateCurrent{}
		}
		m.records[tenant][typ][id] = cur
		m.versions[tenant][typ][id] = append(m.versions[tenant][typ][id], cur)
	}
	for _, r := range d.Records {
		prev := m.records[tenant][r.Type][r.ID]
		hc := prev.HistoryCount + len(r.History)
		if r.HistoryBase >= 0 {
			hc = r.HistoryBase + len(r.History)
		}
		putVersion(r.Type, r.ID, persistence.StateCurrent{Image: slices.Clone(r.Image), HistoryCount: hc, HistoryHead: r.HistoryHead, Position: pos})
	}
	for _, rep := range d.Replacements {
		cur := m.records[tenant][rep.Type][rep.ID]
		if replaced, err := persistence.ReplaceStateRecordValue(cur.Image, rep); err == nil {
			cur.Image, cur.Position = replaced, pos
			putVersion(rep.Type, rep.ID, cur)
		}
	}
	for _, c := range d.Changes {
		if m.changes[tenant][c.App] == nil {
			m.changes[tenant][c.App] = map[string][]byte{}
			m.changeByID[tenant][c.App] = map[string][]byte{}
		}
		if _, seen := m.changeByID[tenant][c.App][c.ID]; !seen {
			cp := slices.Clone(c.Image)
			m.changeByID[tenant][c.App][c.ID] = cp
			m.changeList[tenant][c.App] = append(m.changeList[tenant][c.App], cp)
			if c.Key != "" {
				m.changes[tenant][c.App][c.Key] = cp
			}
		}
	}
	for _, v := range d.Values {
		if m.values[tenant][v.Owner] == nil {
			m.values[tenant][v.Owner] = map[string]json.RawMessage{}
		}
		m.values[tenant][v.Owner][v.Key] = slices.Clone(v.Image)
	}
	for _, u := range d.Unique {
		if m.uniques[tenant][u.Type] == nil {
			m.uniques[tenant][u.Type] = map[string]map[string]string{}
		}
		for rule := range m.uniques[tenant][u.Type] {
			delete(m.uniques[tenant][u.Type][rule], u.ID)
		}
		for rule, key := range u.Keys {
			if m.uniques[tenant][u.Type][rule] == nil {
				m.uniques[tenant][u.Type][rule] = map[string]string{}
			}
			if key != "" {
				m.uniques[tenant][u.Type][rule][u.ID] = key
			}
		}
	}
}
func (m *k58StateStore) StateRecord(_ context.Context, tenant, typ, id string) (json.RawMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.records[tenant][typ][id].Image), nil
}
func (m *k58StateStore) StateCandidates(_ context.Context, tenant, typ string, filters []persistence.StateFilter, after string, limit int, _ int64) ([]string, error) {
	if counter, ok := m.candidateType[typ]; ok {
		counter.Add(1)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []string
	for id, cur := range m.records[tenant][typ] {
		if id <= after {
			continue
		}
		if len(filters) > 0 {
			var doc map[string]any
			_ = json.Unmarshal(cur.Image, &doc)
			ok := true
			for _, f := range filters {
				var v any = doc
				for _, p := range f.Path {
					mp, isMap := v.(map[string]any)
					if !isMap {
						v = nil
						break
					}
					v = mp[p]
				}
				s := ""
				if v != nil {
					s = fmt.Sprint(v)
				}
				if !slices.Contains(f.Values, s) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
		}
		ids = append(ids, id)
	}
	slices.SortFunc(ids, cmp.Compare)
	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}
func (m *k58StateStore) StateHistory(context.Context, string, string, string, int, int) ([]json.RawMessage, error) {
	return nil, nil
}
func (m *k58StateStore) StateChanges(_ context.Context, tenant, app, key, id string, through int) ([][]byte, error) {
	m.changesCalls.Add(1)
	if strings.HasSuffix(key, ":edit") {
		m.editKeyProbes.Add(1)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := m.changeList[tenant][app]
	if through >= 0 && through < len(list) {
		list = list[:through]
	}
	if key != "" {
		if raw := m.changes[tenant][app][key]; len(raw) > 0 && slices.ContainsFunc(list, func(x []byte) bool { return slices.Equal(x, raw) }) {
			return [][]byte{slices.Clone(raw)}, nil
		}
		return nil, nil
	}
	if id != "" {
		if raw := m.changeByID[tenant][app][id]; len(raw) > 0 && slices.ContainsFunc(list, func(x []byte) bool { return slices.Equal(x, raw) }) {
			return [][]byte{slices.Clone(raw)}, nil
		}
		return nil, nil
	}
	out := make([][]byte, len(list))
	for i, raw := range list {
		out[i] = slices.Clone(raw)
	}
	return out, nil
}
func (m *k58StateStore) StateValue(_ context.Context, tenant, owner, key string, _ int64) (json.RawMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.values[tenant][owner][key]), nil
}
func (m *k58StateStore) StateValues(_ context.Context, tenant, owner string, _ int64) (map[string]json.RawMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]json.RawMessage{}
	for k, v := range m.values[tenant][owner] {
		out[k] = slices.Clone(v)
	}
	return out, nil
}
func (m *k58StateStore) BeginStateRead(_ context.Context, tenant string, expected int64) (persistence.StateReadView, error) {
	m.mu.RLock()
	pos := m.seq[tenant]
	m.mu.RUnlock()
	if pos != expected {
		return nil, &persistence.StatePositionError{Tenant: tenant, Position: pos, Applied: expected}
	}
	return &k58ReadView{parent: m, tenant: tenant, pos: pos}, nil
}

type k58ReadView struct {
	parent *k58StateStore
	tenant string
	pos    int64
}

func (v *k58ReadView) Checkpoint(context.Context, string) (persistence.StateDigest, error) {
	return persistence.StateDigest{}, nil
}
func (v *k58ReadView) Position() int64 { return v.pos }
func (v *k58ReadView) Close() error    { return nil }
func (v *k58ReadView) visibleAtLocked(tenant, typ, id string) persistence.StateCurrent {
	list := v.parent.versions[tenant][typ][id]
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Position <= v.pos {
			return list[i]
		}
	}
	return persistence.StateCurrent{}
}
func (v *k58ReadView) StateCurrent(_ context.Context, tenant, typ, id string) (persistence.StateCurrent, error) {
	v.parent.mu.RLock()
	defer v.parent.mu.RUnlock()
	return v.visibleAtLocked(tenant, typ, id), nil
}
func (v *k58ReadView) StateCurrents(_ context.Context, tenant, typ string, ids []string) (map[string]persistence.StateCurrent, error) {
	v.parent.mu.RLock()
	defer v.parent.mu.RUnlock()
	out := map[string]persistence.StateCurrent{}
	for _, id := range ids {
		if cur := v.visibleAtLocked(tenant, typ, id); len(cur.Image) != 0 {
			out[id] = cur
		}
	}
	return out, nil
}
func (v *k58ReadView) StateWindow(context.Context, string, string, persistence.StateWindow) (persistence.StateWindowResult, error) {
	return persistence.StateWindowResult{}, nil
}
func (v *k58ReadView) StateRecord(ctx context.Context, tenant, typ, id string) (json.RawMessage, error) {
	cur, err := v.StateCurrent(ctx, tenant, typ, id)
	return cur.Image, err
}
func (v *k58ReadView) StateCandidates(ctx context.Context, tenant, typ string, filters []persistence.StateFilter, after string, limit int, applied int64) ([]string, error) {
	return v.parent.StateCandidates(ctx, tenant, typ, filters, after, limit, applied)
}
func (v *k58ReadView) StateHistory(context.Context, string, string, string, int, int) ([]json.RawMessage, error) {
	return nil, nil
}
func (v *k58ReadView) StateChanges(ctx context.Context, tenant, app, key, id string, through int) ([][]byte, error) {
	return v.parent.StateChanges(ctx, tenant, app, key, id, through)
}
func (v *k58ReadView) StateValue(ctx context.Context, tenant, owner, key string, through int64) (json.RawMessage, error) {
	return v.parent.StateValue(ctx, tenant, owner, key, through)
}
func (v *k58ReadView) StateValues(ctx context.Context, tenant, owner string, through int64) (map[string]json.RawMessage, error) {
	return v.parent.StateValues(ctx, tenant, owner, through)
}
func (v *k58ReadView) StateUnique(_ context.Context, tenant, typ, rule, key string, _ int64) ([]string, error) {
	v.parent.mu.RLock()
	defer v.parent.mu.RUnlock()
	var ids []string
	for id, k := range v.parent.uniques[tenant][typ][rule] {
		if k == key {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, nil
}

func TestInvariantK58CrossTypeConstraintPruning(t *testing.T) {
	ctx := t.Context()
	live := newInvariantTenant(t, "invariant-k58-pruning")
	const catType = "invariantproject.category"
	const itemType = "invariantproject.item"
	const whType = "invariantproject.warehouse"
	const movType = "invariantproject.movement"
	const balType = movType + ".stock"

	live.accept(t, "build.object.create", build.ObjectType, "CAT", map[string]any{
		"name": "category", "title": "Category",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true},
			{"name": "status", "title": "Status", "type": "text", "required": true},
		},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "CAT", nil)

	live.accept(t, "build.object.create", build.ObjectType, "ITEM", map[string]any{
		"name": "item", "title": "Item", "display": "title",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true},
			{"name": "category", "title": "Category", "type": "reference", "ref": catType, "required": true},
			{"name": "status", "title": "Status", "type": "text", "required": true},
		},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "ITEM", nil)

	live.accept(t, "build.object.create", build.ObjectType, "WAREHOUSE", map[string]any{
		"name": "warehouse", "title": "Warehouse", "display": "title",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true},
			{"name": "nodekind", "title": "Kind", "type": "text", "required": true},
		},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "WAREHOUSE", nil)

	live.accept(t, "build.object.create", build.ObjectType, "MOVEMENT", map[string]any{
		"name": "movement", "title": "Stock movement", "ledger": true,
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true},
			{"name": "material", "title": "Material", "type": "reference", "ref": itemType, "required": true},
			{"name": "warehouse", "title": "Warehouse", "type": "reference", "ref": whType, "required": true},
			{"name": "quantity", "title": "Quantity", "type": "decimal", "required": true},
			{"name": "on", "title": "On", "type": "date", "required": true},
		},
		"balances": []map[string]any{
			{"name": "stock", "title": "Stock", "keys": []string{"material", "warehouse"}, "measures": []string{"quantity"}},
		},
		"constraints": []map[string]any{
			{"name": "nonzero", "kind": "expression", "fields": []string{"quantity"}, "expr": map[string]any{
				"op": "!=", "kind": "boolean", "args": []map[string]any{
					{"kind": "decimal", "ref": "quantity"},
					{"kind": "decimal", "lit": map[string]any{"value": 0}},
				},
			}, "message": "Quantity cannot be zero."},
			{"name": "activeitem", "kind": "expression", "fields": []string{"material"}, "expr": map[string]any{
				"op": "=", "kind": "boolean", "args": []map[string]any{
					{"op": "path", "kind": "text", "path": []string{"status"}, "args": []map[string]any{{"kind": "reference", "ref": "material"}}},
					{"kind": "text", "lit": map[string]any{"value": "active"}},
				},
			}, "message": "Select an active item."},
			{"name": "opencategory", "kind": "expression", "fields": []string{"material"}, "expr": map[string]any{
				"op": "=", "kind": "boolean", "args": []map[string]any{
					{"op": "path", "kind": "text", "path": []string{"category", "status"}, "args": []map[string]any{{"kind": "reference", "ref": "material"}}},
					{"kind": "text", "lit": map[string]any{"value": "open"}},
				},
			}, "message": "Select an open category."},
			{"name": "storagewh", "kind": "expression", "fields": []string{"warehouse"}, "expr": map[string]any{
				"op": "=", "kind": "boolean", "args": []map[string]any{
					{"op": "path", "kind": "text", "path": []string{"nodekind"}, "args": []map[string]any{{"kind": "reference", "ref": "warehouse"}}},
					{"kind": "text", "lit": map[string]any{"value": "storage"}},
				},
			}, "message": "Select a storage warehouse."},
		},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "MOVEMENT", nil)
	live.accept(t, catType+".create", catType, "CAT-1", map[string]any{"title": "Hardware", "status": "open"})
	live.accept(t, itemType+".create", itemType, "LOAD-I", map[string]any{"title": "Bolt", "category": "CAT-1", "status": "active"})
	live.accept(t, whType+".create", whType, "LOAD-W", map[string]any{"title": "Main WH", "nodekind": "storage"})
	live.accept(t, whType+".create", whType, "UNUSED-W", map[string]any{"title": "Spare WH", "nodekind": "storage"})

	state := newK58StateStore(movType, itemType, whType)
	for _, entry := range live.entries {
		id, err := acceptedIdentity(entry.Body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := state.AppendAccepted(ctx, live.ID, entry, id.Key, id.Hash); err != nil {
			t.Fatal(err)
		}
	}
	if err := live.MigrateRuntimeState(ctx, state, nil, live.runtimeOwners()); err != nil {
		t.Fatal(err)
	}
	live.AttachJournal(ctx, state)

	member := live.member(t)
	buildCSV := func(start, count int) []byte {
		var b strings.Builder
		b.WriteString("id,title,material,warehouse,quantity,on\n")
		for i := 0; i < count; i++ {
			id := start + i
			fmt.Fprintf(&b, "M-%04d,Fact %d,Bolt,Main WH,1,2026-10-26\n", id, id)
		}
		return []byte(b.String())
	}

	seedRows, refusal := live.Import(member, movType, buildCSV(0, 60), false, invariantClock)
	if refusal != nil || len(seedRows) != 60 {
		t.Fatalf("seed import: %d %+v", len(seedRows), refusal)
	}

	state.candidateType[movType].Store(0)
	state.candidateType[itemType].Store(0)
	state.candidateType[whType].Store(0)
	state.changesCalls.Store(0)
	state.editKeyProbes.Store(0)

	nextCSV := buildCSV(60, 20)
	nextRows, refusal := live.Import(member, movType, nextCSV, false, invariantClock)
	if refusal != nil || len(nextRows) != 20 {
		t.Fatalf("incremental import: %d %+v", len(nextRows), refusal)
	}
	for _, r := range nextRows {
		if r.Outcome != "ok" {
			t.Fatalf("incremental row failed: %+v", r)
		}
	}
	if scans := state.candidateType[movType].Load(); scans != 0 {
		t.Fatalf("ledger import scanned historical movement candidates %d times, want 0", scans)
	}
	if itemScans := state.candidateType[itemType].Load(); itemScans > 2 {
		t.Fatalf("expected deduplicated item reference resolution (<=2 candidate scans), got %d", itemScans)
	}
	if editProbes := state.editKeyProbes.Load(); editProbes != 0 {
		t.Fatalf("expected immutable ledger import to issue 0 :edit probes, got %d", editProbes)
	}
	if changeProbes := state.changesCalls.Load(); changeProbes > 80 {
		t.Fatalf("expected bounded StateChanges lookups (<=80 for 20 rows), got %d", changeProbes)
	}

	if _, err := live.Submit(member, live.submission(itemType+".edit", itemType, "LOAD-I", map[string]any{"status": "inactive"}), invariantClock); err == nil || !strings.Contains(err.Message, "Select an active item") {
		t.Fatalf("expected 1-hop item constraint refusal, got %+v", err)
	}
	if _, err := live.Submit(member, live.submission(whType+".edit", whType, "LOAD-W", map[string]any{"nodekind": "group"}), invariantClock); err == nil || !strings.Contains(err.Message, "Select a storage warehouse") {
		t.Fatalf("expected 1-hop warehouse constraint refusal, got %+v", err)
	}
	if _, err := live.Submit(member, live.submission(catType+".edit", catType, "CAT-1", map[string]any{"status": "closed"}), invariantClock); err == nil || !strings.Contains(err.Message, "Select an open category") {
		t.Fatalf("expected 2-hop category constraint refusal, got %+v", err)
	}
	if _, err := live.Submit(member, live.submission(whType+".edit", whType, "UNUSED-W", map[string]any{"nodekind": "group"}), invariantClock); err != nil {
		t.Fatalf("editing unreferenced warehouse should succeed, got %+v", err)
	}

	posBeforeRetry := state.Position(live.ID)
	retryRows, refusal := live.Import(member, movType, nextCSV, false, invariantClock)
	if refusal != nil || state.Position(live.ID) != posBeforeRetry || len(retryRows) != 20 {
		t.Fatalf("idempotent retry mismatch: pos=%d/%d err=%+v", state.Position(live.ID), posBeforeRetry, refusal)
	}

	readBal := func(l *invariantTenant) string {
		t.Helper()
		page, err := l.Records(t.Context(), l.member(t), balType, platform.Query{}, invariantClock)
		if err != nil || len(page.Records) != 1 {
			t.Fatalf("balance read: %+v %v", page, err)
		}
		return string(platform.Raw(page.Records[0]))
	}
	liveBal := readBal(live)
	if !strings.Contains(liveBal, `"quantity":80`) {
		t.Fatalf("unexpected stock balance: %s", liveBal)
	}
	entries, err := state.Entries(ctx, live.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	replayed := live.restore(t, entries)
	if got := readBal(replayed); got != liveBal {
		t.Fatalf("replay balance differs: %s != %s", got, liveBal)
	}
}
