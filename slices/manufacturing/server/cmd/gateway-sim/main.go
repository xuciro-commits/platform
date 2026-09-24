// Command gateway-sim plays the plant's edge: a line gateway pushing equipment
// state batches (with a stop now and then) and the ERP poller importing planned
// orders page by page.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"mes"
)

func post(server, token, path string, body any) int {
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, server+path, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Print(err)
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

func main() {
	server := flag.String("server", "http://127.0.0.1:8490", "mes-server URL")
	every := flag.Duration("every", 5*time.Second, "time between gateway batches")
	batches := flag.Int("batches", 0, "stop after this many batches (0: run forever)")
	flag.Parse()

	orders := [][]mes.PlannedOrder{
		{{ERPID: "PO-9001", Product: "P-100", Quantity: 20, Due: "2026-10-02"}, {ERPID: "PO-9002", Product: "P-200", Quantity: 8, Due: "2026-10-03"}},
		{{ERPID: "PO-9003", Product: "P-100", Quantity: 12, Due: "2026-10-06"}},
	}
	cursor := ""
	for i, page := range orders {
		next := fmt.Sprintf("page-%d", i+1)
		fmt.Println("erp page", next, post(*server, "erp", "/v1/connectors/planned-orders",
			mes.PlannedPage{CursorFrom: cursor, CursorTo: next, Orders: page}))
		cursor = next
	}

	resources := []string{"FURNACE-1", "CNC-11", "CNC-12", "CMM-1"}
	for n := 1; *batches == 0 || n <= *batches; n++ {
		start := time.Now().Add(-*every)
		for r, resource := range resources {
			var samples []mes.Sample
			for s := 0; s < 10; s++ {
				state := "run"
				if (n+r)%4 == 0 && s >= 3 && s < 7 {
					state = "down"
				} else if s == 9 && r == 3 {
					state = "idle"
				}
				samples = append(samples, mes.Sample{At: start.Add(time.Duration(s) * *every / 10), State: state})
			}
			fmt.Println("batch", n, resource, post(*server, "gateway-l1", "/v1/connectors/states",
				mes.StateBatch{BatchID: fmt.Sprintf("%s-%d", resource, n), Resource: resource, Samples: samples}))
		}
		if *batches == 0 || n < *batches {
			time.Sleep(*every)
		}
	}
}
