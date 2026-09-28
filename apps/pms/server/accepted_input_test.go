package pms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
)

func TestAcceptedChannelBookingKeepsFactDecisionAndConnectorTogether(t *testing.T) {
	var entries []platformserver.Entry
	hotel, tn := hotelTenant(t, &entries)
	at := time.Date(2026, 9, 28, 20, 0, 0, 0, time.UTC)
	booking := ChannelBooking{MessageID: "ota-accepted-1", ReservationID: "r-accepted-1", Guest: "Guest",
		Stay: Stay{RoomType: "suite", CheckIn: "2026-11-01", CheckOut: "2026-11-02"}, SentAt: at}
	raw, _ := json.Marshal(booking)
	fail := true
	tn.AcceptResult = func(entry platformserver.Entry, _, _ string) ([]byte, error) {
		if len(hotel.facts.Records(tn.ID)) != 0 || count(hotel) != 0 ||
			tn.Connectors(at)[0].LastSeen != nil {
			t.Fatal("channel fact, booking or connector mark escaped before commit")
		}
		if fail {
			return nil, errors.New("append failure")
		}
		entries = append(entries, entry)
		return entry.Body, nil
	}
	if _, refusal := tn.Input(channel.Member, "channel-bookings", raw, at); refusal == nil {
		t.Fatal("append failure accepted a channel booking")
	}
	if len(hotel.facts.Records(tn.ID)) != 0 || count(hotel) != 0 {
		t.Fatal("refused append changed the hotel")
	}
	fail = false
	answer, refusal := tn.Input(channel.Member, "channel-bookings", raw, at)
	if refusal != nil || answer == nil || len(entries) != 1 {
		t.Fatalf("channel booking was not accepted: %v", refusal)
	}
	if len(hotel.facts.Records(tn.ID)) != 1 || count(hotel) != 1 ||
		tn.Connectors(at)[0].LastSeen == nil {
		t.Fatal("committed channel booking omitted a fact, reservation or connector")
	}
	again, refusal := tn.Input(channel.Member, "channel-bookings", raw, at.Add(time.Minute))
	if refusal != nil || again.(*pb.ChangeRecord).GetChangeId() != answer.(*pb.ChangeRecord).GetChangeId() ||
		len(entries) != 1 {
		t.Fatalf("duplicate channel booking reran the decision: %v", refusal)
	}
	var ignored []platformserver.Entry
	platformserver.CheckReplay(t, tn, entries, func() *platformserver.Tenant {
		_, fresh := hotelTenant(t, &ignored)
		return fresh
	})
}

func TestJournalAcceptedChannelBookingCrashAndRestart(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	journal, err := platformserver.OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	id := fmt.Sprintf("hotel-channel-%d", time.Now().UnixNano())
	cleanup, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.Close(ctx)
	defer cleanup.Exec(ctx, `delete from journal where tenant=$1`, id)
	if entries, err := journal.Entries(ctx, id, 0); err != nil || len(entries) != 0 {
		t.Fatalf("new tenant journal is not empty: %v %v", entries, err)
	}
	compose := func() (*Hotel, *platformserver.Tenant) {
		h := New(id, map[string]RoomType{"suite": {Rooms: 1}})
		seat := platformserver.Seat{Subjects: []string{"channel-sim"}, Member: platform.Member{
			ID: "channel-sim", Roles: map[string]string{ID: string(Channel)}}}
		tn, err := platformserver.NewTenant(id, platformserver.NewConsole(id, seat), h)
		if err == nil {
			err = tn.Connect(ChannelConnector("channel-sim"))
		}
		if err != nil {
			t.Fatal(err)
		}
		return h, tn
	}
	liveHotel, live := compose()
	member, _ := live.Member("channel-sim")
	at := time.Date(2026, 9, 28, 21, 0, 0, 0, time.UTC)
	body, _ := json.Marshal(ChannelBooking{MessageID: "channel-crash", ReservationID: "r-crash",
		Guest: "Guest", Stay: Stay{RoomType: "suite", CheckIn: "2026-11-01", CheckOut: "2026-11-02"}, SentAt: at})
	live.AcceptResult = func(entry platformserver.Entry, key, hash string) ([]byte, error) {
		if _, err := journal.AppendAccepted(ctx, id, entry, key, hash); err != nil {
			return nil, err
		}
		return nil, errors.New("simulated crash after append")
	}
	if _, refusal := live.Input(member, "channel-bookings", body, at); refusal == nil {
		t.Fatal("interrupted answer returned success")
	}
	if len(liveHotel.facts.Records(id)) != 0 || live.Connectors(at)[0].LastSeen != nil {
		t.Fatal("unapplied channel result changed the hotel")
	}
	reopened, err := platformserver.OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("committed booking did not survive restart: %v %v", entries, err)
	}
	recoveredHotel, recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if len(recoveredHotel.facts.Records(id)) != 1 || recovered.Connectors(at)[0].LastSeen == nil {
		t.Fatal("recovered booking omitted its fact or connector mark")
	}
	recovered.AcceptResult = func(entry platformserver.Entry, key, hash string) ([]byte, error) {
		return reopened.AppendAccepted(ctx, id, entry, key, hash)
	}
	if answer, refusal := recovered.Input(member, "channel-bookings", body, at.Add(time.Minute)); refusal != nil ||
		answer.(*pb.ChangeRecord).GetChangeId() == "" || reopened.Position(id) != 1 {
		t.Fatalf("retry did not return the saved booking: %v", refusal)
	}
	platformserver.CheckReplay(t, recovered, entries, func() *platformserver.Tenant {
		_, fresh := compose()
		return fresh
	})
}
