package host

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK42PackageDataFillsInBatchesAndOutlivesDemoData(t *testing.T) {
	admin := protocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin", build.ID: ontology.BuilderRole}}}
	live := newInvariantTenant(t, "invariant-k42", admin)
	const typ = "build.batchproviderorder"
	raw, err := os.ReadFile(fixturePath(t, "production-provider"))
	if err != nil {
		t.Fatal(err)
	}
	var offer platform.PackageDescriptor
	if err := json.Unmarshal(raw, &offer); err != nil {
		t.Fatal(err)
	}
	chunk := func(from, to int) json.RawMessage {
		rows := []map[string]any{}
		for i := from; i <= to; i++ {
			rows = append(rows, map[string]any{"id": fmt.Sprintf("data-%d", i), "number": fmt.Sprintf("D-%d", i), "product": "sample", "quantity": 1, "unitcost": 1})
		}
		return platform.Raw(map[string]any{"kind": "data", "type": typ, "rows": rows})
	}
	demo := platform.Raw(map[string]any{"type": typ, "id": "demo-order-1", "values": map[string]any{"number": "DEMO-1", "product": "sample", "quantity": 2, "unitcost": 5}})
	offer.Blueprint.Seed = []json.RawMessage{demo, chunk(1, 300), chunk(301, 600), chunk(601, 900)}
	member, _ := live.Member("admin")
	apply := func(schema, version string, descriptor platform.PackageDescriptor) {
		t.Helper()
		if version != "" {
			preview, err := live.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(descriptor)})
			if err != nil {
				t.Fatal(err)
			}
			imported := live.submission(packages.SchemaPackageImport, protocol.PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "digest": preview.Digest})
			imported.Authority, imported.PrincipalId = "platform", member.ID
			if _, refusal := live.Submit(member, imported, invariantClock); refusal != nil {
				t.Fatalf("import %s: %s", version, refusal.Message)
			}
		}
		request := live.submission(schema, protocol.PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version})
		request.Authority, request.PrincipalId = "platform", member.ID
		if _, refusal := live.Submit(member, request, invariantClock); refusal != nil {
			t.Fatalf("%s: %s", schema, refusal.Message)
		}
		live.Work(invariantClock)
	}
	field := func(tenant *invariantTenant, id, name string) (any, bool) {
		t.Helper()
		view, refusal := tenant.RecordOf(t.Context(), tenant.member(t), typ, id, invariantClock)
		if refusal != nil {
			return nil, false
		}
		var record map[string]any
		_ = json.Unmarshal(platform.Raw(view.Record), &record)
		return record[name], true
	}
	present := func(tenant *invariantTenant, ids ...string) {
		t.Helper()
		for _, id := range ids {
			if archived, ok := field(tenant, id, "archived"); !ok || archived == true {
				t.Fatalf("package data row %s must be present", id)
			}
		}
	}

	apply(packages.SchemaPackageInstall, "0.1.6", func() platform.PackageDescriptor { offer.Version = "0.1.6"; return offer }())
	present(live, "demo-order-1", "data-1", "data-300", "data-301", "data-600", "data-900")

	live.accept(t, typ+".edit", typ, "data-2", map[string]any{"product": "edited"})
	apply(packages.SchemaPackageClearDemo, "", offer)
	if archived, _ := field(live, "demo-order-1", "archived"); archived != true {
		t.Fatal("clearing demo data archives the demo record")
	}
	present(live, "data-1", "data-900")

	offer.Version = "0.1.7"
	offer.Blueprint.Seed = []json.RawMessage{demo, chunk(1, 300), chunk(301, 600), chunk(601, 950)}
	apply(packages.SchemaPackageUpgrade, offer.Version, offer)
	present(live, "data-901", "data-950")
	if product, _ := field(live, "data-2", "product"); product != "edited" {
		t.Fatalf("an upgrade adds missing rows and keeps edited ones, got %v", product)
	}
	if archived, _ := field(live, "demo-order-1", "archived"); archived != true {
		t.Fatal("an upgrade does not bring cleared demo data back")
	}

	before := live.recordImage(t)
	replayed := live.restore(t, live.entries, admin)
	if after := replayed.recordImage(t); !reflect.DeepEqual(before, after) {
		t.Fatal("replay must rebuild the package data rows")
	}
}
