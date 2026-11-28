package host

import (
	"fmt"
	goruntime "runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func BenchmarkLedgerTenThousandReplay(b *testing.B) {
	for n := 0; n < b.N; n++ {
		live := newInvariantTenant(b, fmt.Sprintf("ledger-load-%d", n))
		const fact = "invariantproject.posting"
		const balance = fact + ".balance"
		live.accept(b, "build.object.create", build.ObjectType, "POSTING", map[string]any{
			"name": "posting", "title": "Posting", "ledger": true,
			"fields":   []map[string]any{{"name": "bucket", "title": "Bucket", "type": "text", "required": true}, {"name": "qty", "title": "Quantity", "type": "integer", "required": true}},
			"balances": []map[string]any{{"name": "balance", "title": "Balance", "keys": []string{"bucket"}, "measures": []string{"qty"}}},
		})
		live.accept(b, "build.object.publish", build.ObjectType, "POSTING", nil)
		var csv strings.Builder
		csv.WriteString("id,bucket,qty,on\n")
		for i := 0; i < 10000; i++ {
			fmt.Fprintf(&csv, "F%05d,K%d,1,2026-10-31\n", i, i%20)
		}
		begin := time.Now()
		rows, refusal := live.Import(live.member(b), fact, []byte(csv.String()), false, invariantClock)
		if refusal != nil || len(rows) != 10000 {
			b.Fatalf("ledger import: %d %v", len(rows), refusal)
		}
		for _, row := range rows {
			if row.Outcome != "ok" {
				b.Fatalf("import row: %+v", row)
			}
		}
		b.ReportMetric(float64(time.Since(begin).Microseconds())/1000, "import-ms")
		read := func(l *invariantTenant) []byte {
			page, refusal := l.Records(b.Context(), l.member(b), balance, platform.Query{Limit: 100}, invariantClock)
			if refusal != nil || len(page.Records) != 20 {
				b.Fatalf("balance read: %+v %v", page, refusal)
			}
			return platform.Raw(page.Records)
		}
		before := read(live)
		if strings.Count(string(before), `"qty":500`) != 20 {
			b.Fatalf("independent quantity oracle: %s", before)
		}
		var post, reads []float64
		for i := 0; i < 20; i++ {
			at := time.Now()
			live.accept(b, fact+".create", fact, fmt.Sprintf("P%d", i), map[string]any{"bucket": fmt.Sprintf("K%d", i), "qty": 1})
			post = append(post, float64(time.Since(at).Microseconds())/1000)
			at = time.Now()
			read(live)
			reads = append(reads, float64(time.Since(at).Microseconds())/1000)
		}
		before = read(live)
		if strings.Count(string(before), `"qty":501`) != 20 {
			b.Fatalf("independent quantity oracle: %s", before)
		}
		if diff, err := live.Projection.ReconcileLedger(fact); err != nil || len(diff) != 0 {
			b.Fatalf("reconcile: %v %v", diff, err)
		}
		var memory goruntime.MemStats
		goruntime.ReadMemStats(&memory)
		b.ReportMetric(float64(memory.HeapAlloc)/(1024*1024), "live-heap-MiB")
		begin = time.Now()
		fresh := live.restore(b, live.entries)
		after := read(fresh)
		if string(after) != string(before) {
			b.Fatal("full replay changed balance bytes")
		}
		b.ReportMetric(float64(time.Since(begin).Microseconds())/1000, "replay-ms")
		sort.Float64s(post)
		sort.Float64s(reads)
		b.ReportMetric(post[18], "posting-p95-ms")
		b.ReportMetric(reads[18], "read-p95-ms")
	}
}
