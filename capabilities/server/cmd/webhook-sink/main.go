// Command webhook-sink is a webhook receiver for the local stack and its
// rehearsal (ADR-0014): it checks Standard Webhooks signatures with the secret
// in WEBHOOK_SECRET, keeps one copy per webhook-id, and lists what it kept.
//
//	POST /hook      a webhook; 204 when kept (or already kept), 401 on a bad signature
//	POST /erp       an ERP's confirmation API (#101): answers {"confirmation": …}, the
//	                same number for the same key, or 422 when no planned order is named
//	GET  /received  {"calls": n, "kept": {"<webhook-id>": <body>}, "confirmations": {"<webhook-id>": "CONF-…"}}
//	POST /fail?on=true|false   answer 503 to every webhook until switched off
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
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
	confirmations := map[string]string{}
	verified := func(r *http.Request, body []byte) bool {
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(r.Header.Get("webhook-id") + "." + r.Header.Get("webhook-timestamp") + "." + string(body)))
		return hmac.Equal([]byte(r.Header.Get("webhook-signature")), []byte("v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil))))
	}
	http.HandleFunc("POST /erp", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		var c struct {
			Data struct{ Order, Planned string }
		}
		json.Unmarshal(body, &c)
		id := r.Header.Get("webhook-id")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case !verified(r, body):
			w.WriteHeader(http.StatusUnauthorized)
		case failing:
			w.WriteHeader(http.StatusServiceUnavailable)
		case c.Data.Planned == "":
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{"error": "no planned order to confirm against"})
		default:
			if confirmations[id] == "" {
				confirmations[id] = fmt.Sprintf("CONF-%d", 100000+len(confirmations)+1)
			}
			json.NewEncoder(w).Encode(map[string]string{"confirmation": confirmations[id]})
		}
	})
	http.HandleFunc("POST /hook", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		id := r.Header.Get("webhook-id")
		mu.Lock()
		defer mu.Unlock()
		calls++
		switch {
		case !verified(r, body):
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
		json.NewEncoder(w).Encode(map[string]any{"calls": calls, "kept": kept, "confirmations": confirmations})
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
