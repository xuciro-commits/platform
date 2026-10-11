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

func TestChannelInputUsesAcceptedResultsWithoutAcceptCallback(t *testing.T) {
	for _, storage := range []string{"memory", "record"} {
		t.Run(storage, func(t *testing.T) {
			var entries []platformserver.Entry
			hotel, tn := hotelTenant(t, &entries)
			at := time.Date(2026, 10, 11, 20, 0, 0, 0, time.UTC)
			tn.Record = nil
			if storage == "record" {
				tn.Record = func(entry platformserver.Entry) {
					if entry.Kind != "accepted-result" {
						t.Fatalf("connector wrote the legacy input entry: %s", entry.Kind)
					}
					if len(hotel.facts.Records(tn.ID)) != len(entries) || count(hotel) != len(entries) {
						t.Fatal("channel observation or reservation escaped before acceptance")
					}
					if seen := tn.Connectors(at)[0].LastSeen; len(entries) == 0 && seen != nil ||
						len(entries) == 1 && (seen == nil || !seen.Equal(at)) {
						t.Fatal("connector cursor escaped before booking acceptance")
					}
					entries = append(entries, entry)
				}
			}
			booking := ChannelBooking{MessageID: "channel-storage", ReservationID: "r-storage", Guest: "Guest",
				Stay: Stay{RoomType: "suite", CheckIn: "2026-11-01", CheckOut: "2026-11-02"}, SentAt: at}
			body, _ := json.Marshal(booking)
			answer, refusal := tn.Input(channel.Member, "channel-bookings", body, at)
			record, ok := answer.(*pb.ChangeRecord)
			if refusal != nil || !ok || len(hotel.facts.Records(tn.ID)) != 1 || count(hotel) != 1 {
				t.Fatalf("channel booking was not accepted: %v, %v", answer, refusal)
			}
			cursor := tn.Connectors(at)[0].LastSeen
			if cursor == nil {
				t.Fatal("accepted booking omitted the connector cursor")
			}
			booking.MessageID, booking.ReservationID = "channel-sold-out", "r-sold-out"
			soldOut, _ := json.Marshal(booking)
			if _, refusal = tn.Input(channel.Member, "channel-bookings", soldOut, at.Add(time.Minute)); refusal == nil {
				t.Fatal("a second booking was accepted into the only suite")
			}
			if len(hotel.facts.Records(tn.ID)) != 1 || count(hotel) != 1 ||
				!tn.Connectors(at)[0].LastSeen.Equal(*cursor) {
				t.Fatal("refused booking changed facts, reservations or connector cursor")
			}
			_, againRefusal := tn.Input(channel.Member, "channel-bookings", soldOut, at.Add(2*time.Minute))
			if againRefusal == nil || againRefusal.Error() != refusal.Error() {
				t.Fatalf("refused retry changed its saved answer: %v", againRefusal)
			}
			again, againRefusal := tn.Input(channel.Member, "channel-bookings", body, at.Add(3*time.Minute))
			if againRefusal != nil || again.(*pb.ChangeRecord).GetChangeId() != record.GetChangeId() ||
				!tn.Connectors(at)[0].LastSeen.Equal(*cursor) {
				t.Fatalf("successful retry changed its decision or cursor: %v", againRefusal)
			}
			if storage == "record" {
				if len(entries) != 2 {
					t.Fatalf("retries wrote extra input results: %d", len(entries))
				}
				platformserver.CheckReplay(t, tn, entries, func() *platformserver.Tenant {
					var ignored []platformserver.Entry
					_, fresh := hotelTenant(t, &ignored)
					return fresh
				})
			}
		})
	}
}

func TestLegacyChannelInputRetryUsesAcceptedResult(t *testing.T) {
	var entries []platformserver.Entry
	hotel, tn := hotelTenant(t, &entries)
	at := time.Date(2026, 10, 11, 21, 0, 0, 0, time.UTC)
	body, _ := json.Marshal(ChannelBooking{MessageID: "channel-legacy", ReservationID: "r-legacy", Guest: "Guest",
		Stay: Stay{RoomType: "suite", CheckIn: "2026-11-01", CheckOut: "2026-11-02"}, SentAt: at})
	principal, _ := json.Marshal(channel.Member)
	legacy := platformserver.Entry{App: ID, Kind: "channel-bookings", Principal: principal, Body: body, At: at}
	if err := tn.Replay([]platformserver.Entry{legacy}); err != nil {
		t.Fatal(err)
	}
	if count(hotel) != 1 || len(hotel.facts.Records(tn.ID)) != 1 {
		t.Fatal("legacy input did not restore its reservation and fact")
	}
	answer, refusal := tn.Input(channel.Member, "channel-bookings", body, at.Add(time.Minute))
	record, ok := answer.(*pb.ChangeRecord)
	if refusal != nil || !ok || record.GetChangeId() == "" {
		t.Fatalf("retry of a restored legacy booking failed: %v, %v", answer, refusal)
	}
	if len(entries) != 1 || entries[0].Kind != "accepted-result" || count(hotel) != 1 || len(hotel.facts.Records(tn.ID)) != 1 {
		t.Fatal("legacy retry reran the booking or used a legacy entry")
	}
	platformserver.CheckReplay(t, tn, append([]platformserver.Entry{legacy}, entries...), func() *platformserver.Tenant {
		var ignored []platformserver.Entry
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
