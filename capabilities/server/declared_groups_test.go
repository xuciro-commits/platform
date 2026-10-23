package platformserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
)

func declaredRecord(t *testing.T, tn *Tenant, member, typ, id string, at time.Time) map[string]any {
	t.Helper()
	answer, err := tn.RecordOf(memberOf(t, tn, member), typ, id, at)
	if err != nil {
		t.Fatalf("%s/%s: %v", typ, id, err)
	}
	var record map[string]any
	if err := json.Unmarshal(platform.Raw(answer.Record), &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestDeclaredGroupReservationUsesIndependentProviderReceipts(t *testing.T) {
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	clock := at
	provider := declaredIndustryTenant(t, "lodging-provider", "hospitality", []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("manager", "build:hotelmanager"), seatOf("service", "build:channel")}, at)
	decide(t, provider, "manager", "build", "build.pmsroomtype.create", "build.pmsroomtype", "daily", map[string]any{"name": "Provider daily rooms", "unit": "day", "capacity": 3, "minunits": 1, "allowance": 0, "oversell": false, "active": true}, at)
	host := NewHost(Tokens(map[string]string{"lodging-service": "service"}), provider)
	host.Now = func() time.Time { return clock }
	handler := host.Handler()
	posts := map[string][]string{}
	rejectRelease := false
	rejectConfirmation := 0
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		if r.Method == http.MethodPost {
			posts[r.URL.Path] = append(posts[r.URL.Path], r.Header.Get("Idempotency-Key"))
			if rejectConfirmation > 0 && strings.Contains(r.URL.Path, "/confirm/") {
				rejectConfirmation--
				if rejectConfirmation == 0 {
					WriteJSON(w, http.StatusConflict, map[string]string{"reason": "This room confirmation was refused by the provider"})
					return
				}
			}
			if rejectRelease && strings.Contains(r.URL.Path, "/release/") {
				WriteJSON(w, http.StatusConflict, map[string]string{"reason": "Provider requires the operator to resolve its release refusal"})
				return
			}
		}
		handler.ServeHTTP(w, r)
	})
	publisher := seatOf("publisher", "build:publisher")
	publisher.Member.Grants = append(publisher.Member.Grants, platform.Grant{App: "ai", Role: "user"})
	for _, role := range []string{"publisher", "integrator", "channel"} {
		publisher.Member.Grants = append(publisher.Member.Grants, platform.Grant{App: "build", Role: role})
	}
	seats := []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("sales", "build:sales"), seatOf("manager", "build:salesmanager"), publisher}
	var entries []Entry
	consumer := declaredIndustryTenant(t, "sales-consumer", "hospitality", seats, at, declaredFixtureOptions{Entries: &entries, Lodging: upstream, Activator: "publisher"})
	files := &memoryFiles{}
	consumer.Files = files
	consumer.PullSources(at)
	offers, err := consumer.Records(memberOf(t, consumer, "sales"), "build.crmlodgingoffer", platform.Query{Limit: 20}, at)
	if err != nil || offers.Total != 1 {
		t.Fatalf("provider directory: %d %v", offers.Total, err)
	}
	var offer map[string]any
	_ = json.Unmarshal(platform.Raw(offers.Records[0]), &offer)
	decide(t, consumer, "sales", "build", "build.crmcustomer.create", "build.crmcustomer", "customer", map[string]any{"name": "Group customer", "kind": "company"}, at)
	decide(t, consumer, "sales", "build", "build.crmopportunity.create", "build.crmopportunity", "sale", map[string]any{"customer": "customer", "title": "Conference"}, at)
	decide(t, consumer, "sales", "build", "build.crmgroup.create", "build.crmgroup", "group", map[string]any{"opportunity": "sale", "offer": offer["id"], "quantity": 2, "unit": "day", "dayfrom": "2026-10-22", "dayuntil": "2026-10-24", "expires": "2026-10-21T10:00:00Z"}, at)
	advance := func(typ, id, state string) {
		t.Helper()
		for n := 0; n < 12; n++ {
			if declaredRecord(t, consumer, "sales", typ, id, at)["state"] == state {
				return
			}
			clock = clock.Add(time.Second)
			consumer.Work(clock)
			consumer.Dispatch(clock)
		}
		t.Fatalf("%s/%s did not reach %s: %s; fault=%+v", typ, id, state, platform.Raw(declaredRecord(t, consumer, "sales", typ, id, at)), consumer.fault.Load())
	}
	advance("build.crmgroup", "group", "held")
	local, err := consumer.Records(memberOf(t, consumer, "builder"), "build.pmsreservation", platform.Query{}, at)
	remote, remoteErr := provider.Records(memberOf(t, provider, "service"), "build.pmsreservation", platform.Query{}, at)
	if err != nil || remoteErr != nil || local.Total != 0 || remote.Total != 2 {
		t.Fatalf("cross-authority hold: local=%d remote=%d errors=%v/%v", local.Total, remote.Total, err, remoteErr)
	}
	for _, row := range remote.Records {
		var booking map[string]any
		_ = json.Unmarshal(platform.Raw(row), &booking)
		if booking["state"] != "held" || !strings.HasPrefix(booking["id"].(string), consumer.ID+"-") {
			t.Fatalf("provider did not retain the original consumer identity: %+v", booking)
		}
	}
	decide(t, consumer, "sales", "build", "build.crmopportunity.win", "build.crmopportunity", "sale", nil, at)
	if declaredRecord(t, consumer, "sales", "build.crmopportunity", "sale", at)["state"] != "closing" {
		t.Fatal("sale reported a win before receiving the provider's room confirmations")
	}
	advance("build.crmopportunity", "sale", "won")
	remote, remoteErr = provider.Records(memberOf(t, provider, "service"), "build.pmsreservation", platform.Query{}, at)
	if remoteErr != nil || remote.Total != 2 {
		t.Fatal(remoteErr)
	}
	for _, row := range remote.Records {
		var booking map[string]any
		_ = json.Unmarshal(platform.Raw(row), &booking)
		if booking["state"] != "booked" {
			t.Fatalf("sale won without an actual provider confirmation: %+v", booking)
		}
	}
	if len(posts) != 4 {
		t.Fatalf("expected two original holds and two confirmations: %+v", posts)
	}
	createGroup := func(sale, group string, quantity int) {
		t.Helper()
		decide(t, consumer, "sales", "build", "build.crmopportunity.create", "build.crmopportunity", sale, map[string]any{"customer": "customer", "title": sale}, at)
		decide(t, consumer, "sales", "build", "build.crmgroup.create", "build.crmgroup", group, map[string]any{"opportunity": sale, "offer": offer["id"], "quantity": quantity, "unit": "day", "dayfrom": "2026-10-22", "dayuntil": "2026-10-24", "expires": "2026-10-21T10:00:00Z"}, at)
	}
	roomsOf := func(plan string) []map[string]any {
		t.Helper()
		page, err := consumer.Records(memberOf(t, consumer, "sales"), "build.crmroom", platform.Query{Domain: platform.Raw([]any{[]any{"plan", "=", plan}}), Limit: 20}, at)
		if err != nil {
			t.Fatal(err)
		}
		var rows []map[string]any
		for _, raw := range page.Records {
			var row map[string]any
			_ = json.Unmarshal(platform.Raw(raw), &row)
			rows = append(rows, row)
		}
		return rows
	}
	// One remaining room can be held, while the second must be refused.
	// A rejected release remains visible until an authorized person retries it.
	rejectRelease = true
	createGroup("partial-sale", "partial", 2)
	advance("build.crmgroup", "partial", "needsreview")
	var unresolved map[string]any
	for _, room := range roomsOf("partial") {
		if room["state"] == "releaserefused" {
			unresolved = room
		}
	}
	if unresolved == nil {
		t.Fatal("the failed compensation lost its original room request")
	}
	rejectRelease = false
	decide(t, consumer, "sales", "build", "build.crmroom.release", "build.crmroom", unresolved["id"].(string), map[string]any{"bookingkey": unresolved["bookingkey"]}, at)
	advance("build.crmgroup", "partial", "failed")
	if declaredRecord(t, provider, "service", "build.pmsreservation", unresolved["bookingkey"].(string), at)["state"] != "released" {
		t.Fatal("the partial hold retained provider occupancy after correction")
	}

	// Lose the HTTP reply after the provider has accepted a real hold. The
	// original read resolves that same request; a new hold is never invented.
	loseReply := true
	consumer.Outbound = func(request *http.Request, _ bool) (*http.Response, error) {
		response, err := http.DefaultClient.Do(request)
		if err == nil && loseReply && strings.Contains(request.URL.Path, "/reserve/") {
			loseReply = false
			response.Body.Close()
			return nil, io.ErrUnexpectedEOF
		}
		return response, err
	}
	createGroup("unknown-sale", "unknown", 1)
	consumer.Work(at.Add(time.Second))
	consumer.Dispatch(at.Add(time.Second))
	var original platform.ConfirmationRun
	for _, run := range platform.Records[platform.ConfirmationRun](consumer.automation("build", false)) {
		if len(run.Parties) == 1 && run.Parties[0].State == "unknown" {
			original = run
		}
	}
	if original.ID == "" || loseReply {
		t.Fatal("the lost delivery answer was presented as a provider refusal or success")
	}
	consumer.ReconcileConfirmations(at.Add(2 * time.Second))
	advance("build.crmgroup", "unknown", "held")
	checked, _ := platform.Get[platform.ConfirmationRun](consumer.automation("build", false), original.ID)
	if checked.Completion == "" || len(checked.Parties[0].Differences) != 1 || !checked.Parties[0].Differences[0].Matched {
		t.Fatalf("the original request was not resolved by its provider evidence: %+v", checked)
	}
	unknownRoom := roomsOf("unknown")[0]
	holdPath := "/v1/protocols/lodging.stays/2/reserve/" + unknownRoom["bookingkey"].(string)
	if len(posts[holdPath]) != 1 || posts[holdPath][0] != original.Parties[0].Intent {
		t.Fatalf("unknown reconciliation invented a different provider request: %+v", posts[holdPath])
	}
	decide(t, consumer, "sales", "build", "build.crmopportunity.lose", "build.crmopportunity", "unknown-sale", nil, at)
	advance("build.crmgroup", "unknown", "released")
	if declaredRecord(t, provider, "service", "build.pmsreservation", unknownRoom["bookingkey"].(string), at)["state"] != "released" {
		t.Fatal("lost opportunity did not release its original provider room")
	}
	decide(t, provider, "manager", "build", "build.pmsreservation.create", "build.pmsreservation", "last-room", map[string]any{"guest": "Provider capacity check", "roomtype": "daily", "unit": "day", "mode": "booked", "dayfrom": "2026-10-22", "dayuntil": "2026-10-24"}, clock)
	createGroup("unavailable-sale", "unavailable", 1)
	advance("build.crmgroup", "unavailable", "failed")
	if rooms := roomsOf("unavailable"); len(rooms) != 1 || rooms[0]["state"] != "holdrefused" {
		t.Fatal("a fully unavailable group did not retain its actual provider refusal")
	}
	decide(t, provider, "manager", "build", "build.pmsreservation.cancel", "build.pmsreservation", "last-room", nil, clock)
	// A partially confirmed win must cancel its booked room and release the
	// other hold. A later corrected win creates a fresh closing attempt.
	decide(t, consumer, "sales", "build", "build.crmopportunity.create", "build.crmopportunity", "corrected-sale", map[string]any{"customer": "customer", "title": "Corrected conference"}, clock)
	groupInput := map[string]any{"opportunity": "corrected-sale", "offer": offer["id"], "quantity": 2, "unit": "day", "dayfrom": "2026-10-25", "dayuntil": "2026-10-27", "expires": "2026-10-21T10:00:00Z"}
	decide(t, consumer, "sales", "build", "build.crmgroup.create", "build.crmgroup", "confirm-partial", groupInput, clock)
	advance("build.crmgroup", "confirm-partial", "held")
	rejectConfirmation = 2
	decide(t, consumer, "sales", "build", "build.crmopportunity.win", "build.crmopportunity", "corrected-sale", nil, clock)
	advance("build.crmgroup", "confirm-partial", "failed")
	advance("build.crmopportunity", "corrected-sale", "open")
	outcomes := map[string]int{}
	for _, room := range roomsOf("confirm-partial") {
		booking := declaredRecord(t, provider, "service", "build.pmsreservation", room["bookingkey"].(string), clock)
		outcomes[booking["state"].(string)]++
	}
	if outcomes["canceled"] != 1 || outcomes["released"] != 1 {
		t.Fatalf("partial confirmation did not compensate each actual provider outcome: %+v", outcomes)
	}
	decide(t, consumer, "sales", "build", "build.crmgroup.create", "build.crmgroup", "corrected-group", groupInput, clock)
	advance("build.crmgroup", "corrected-group", "held")
	decide(t, consumer, "sales", "build", "build.crmopportunity.win", "build.crmopportunity", "corrected-sale", nil, clock)
	advance("build.crmopportunity", "corrected-sale", "won")
	createGroup("expired-sale", "expired", 1)
	advance("build.crmgroup", "expired", "held")
	clock = at.Add(time.Hour + time.Second)
	provider.Work(clock)
	consumer.Work(clock)
	consumer.Dispatch(clock)
	advance("build.crmgroup", "expired", "released")
	for _, room := range roomsOf("expired") {
		if declaredRecord(t, provider, "service", "build.pmsreservation", room["bookingkey"].(string), clock)["state"] != "released" {
			t.Fatal("the group cutoff retained an expired provider hold")
		}
	}
	recovered := composeDeclaredTenant(t, consumer.ID, seats)
	recovered.Files = files
	if err := recovered.Replay(entries); err != nil || snapshot(consumer) != snapshot(recovered) {
		t.Fatalf("group receipts changed on recovery: %v", err)
	}
}
