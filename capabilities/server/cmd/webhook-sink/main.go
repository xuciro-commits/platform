// Command webhook-sink is a webhook receiver for the local stack and its
// rehearsal (ADR-0014): it checks Standard Webhooks signatures with the secret
// in WEBHOOK_SECRET, keeps one copy per webhook-id, and lists what it kept.
//
//	POST /hook      a webhook; 204 when kept (or already kept), 401 on a bad signature
//	GET  /received  {"calls": n, "kept": {"<webhook-id>": <body>}}
//	POST /fail?on=true|false   answer 503 to every webhook until switched off
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:8080", "listen address")
	flag.Parse()
	secret := []byte(os.Getenv("WEBHOOK_SECRET"))
	var mu sync.Mutex
	kept, calls, failing := map[string]json.RawMessage{}, 0, false
	http.HandleFunc("POST /hook", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		id, stamp := r.Header.Get("webhook-id"), r.Header.Get("webhook-timestamp")
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(id + "." + stamp + "." + string(body)))
		mu.Lock()
		defer mu.Unlock()
		calls++
		switch {
		case !hmac.Equal([]byte(r.Header.Get("webhook-signature")), []byte("v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))):
			w.WriteHeader(http.StatusUnauthorized)
		case failing:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			if _, seen := kept[id]; !seen {
				kept[id] = body
			}
			w.WriteHeader(http.StatusNoContent)
		}
	})
	http.HandleFunc("GET /received", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"calls": calls, "kept": kept})
	})
	http.HandleFunc("POST /fail", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		failing = r.URL.Query().Get("on") == "true"
		w.WriteHeader(http.StatusNoContent)
	})
	log.Printf("webhook-sink on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
