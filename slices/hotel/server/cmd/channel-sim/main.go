// Command channel-sim plays an OTA channel manager delivering bookings to the
// hotel, including the duplicate deliveries real channels produce.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"hotel"
)

func main() {
	server := flag.String("server", "http://127.0.0.1:8480", "hotel-server URL")
	token := flag.String("token", "channel-a", "channel bearer token")
	count := flag.Int("bookings", 3, "distinct bookings to deliver, each twice")
	flag.Parse()
	for i := 1; i <= *count; i++ {
		b := hotel.ChannelBooking{MessageID: fmt.Sprintf("ota-%d", i), ReservationID: fmt.Sprintf("ota-%d", i),
			Guest: fmt.Sprintf("OTA Guest %d", i), SentAt: time.Now(),
			Stay: hotel.Stay{RoomType: "suite", CheckIn: "2026-12-01", CheckOut: "2026-12-03"}}
		for delivery := 1; delivery <= 2; delivery++ {
			body, _ := json.Marshal(b)
			req, _ := http.NewRequest(http.MethodPost, *server+"/v1/connectors/channel-bookings", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+*token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Fatal(err)
			}
			reply, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fmt.Printf("%s delivery %d: %d %s", b.MessageID, delivery, resp.StatusCode, reply)
		}
	}
}
