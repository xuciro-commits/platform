package platformserver

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"platformserver/platform"
)

func TestPostgresDeclaredHourlyCapacityInitialization(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	id := fmt.Sprintf("hourly-initialization-%d", time.Now().UnixNano())
	j, err := OpenJournal(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer forgetJournal(t, j, id)
	at := time.Now().UTC().Truncate(time.Microsecond)
	tn := declaredIndustryTenant(t, id, "hospitality", []Seat{seatOf("builder", "build:builder", "platform:admin")}, at, declaredFixtureOptions{Journal: j})
	t.Log("Published the original hotel package on PostgreSQL; creating its 168 hourly slots")
	decide(t, tn, "builder", "build", "build.pmsroomtype.create", "build.pmsroomtype", "hourly", map[string]any{
		"name": "Hourly room", "unit": "hour", "capacity": 1, "minunits": 1, "allowance": 0, "oversell": false, "active": true,
	}, at)
	rows, refusal := tn.Records(memberOf(t, tn, "builder"), "build.pmsslot", platform.Query{Limit: 200}, at)
	if refusal != nil || len(rows.Records) != 168 || rows.Total != 168 {
		t.Fatalf("hourly capacity: %d records, total %d: %v", len(rows.Records), rows.Total, refusal)
	}
}
