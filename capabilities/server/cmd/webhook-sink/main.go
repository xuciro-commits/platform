// Command webhook-sink is a webhook receiver for the local stack and its
// rehearsal (ADR-0014): it checks Standard Webhooks signatures with the secret
// in WEBHOOK_SECRET, keeps one copy per webhook-id, and lists what it kept. It
// is also a mail server (SMTP on -smtp) that keeps one message per Message-ID.
//
//	POST /hook      a webhook; 204 when kept (or already kept), 401 on a bad signature
//	POST /erp       an ERP's confirmation API (#101): answers {"confirmation": …}, the
//	                same number for the same key, or 422 when no planned order is named
//	GET  /received  {"calls": n, "kept": {"<webhook-id>": <body>}, "confirmations": {"<webhook-id>": "CONF-…"}}
//	POST /fail?on=true|false   answer 503 to every webhook until switched off
//	GET  /mail      [{"messageId", "to", "from", "subject", "text"}], oldest first
package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/textproto"
	"os"
	"strings"
	"sync"
)

// message is one mail as the sink keeps it.
type message struct {
	MessageID string `json:"messageId"`
	To        string `json:"to"`
	From      string `json:"from"`
	Subject   string `json:"subject"`
	Text      string `json:"text"`
}

func main() {
	addr := flag.String("addr", "0.0.0.0:8080", "listen address")
	smtpAddr := flag.String("smtp", "0.0.0.0:2525", "SMTP listen address")
	flag.Parse()
	secret := []byte(os.Getenv("WEBHOOK_SECRET"))
	var mu sync.Mutex
	kept, calls, failing := map[string]json.RawMessage{}, 0, false
	confirmations := map[string]string{}
	mails := []message{}
	go serveSMTP(*smtpAddr, func(m message) {
		mu.Lock()
		defer mu.Unlock()
		for _, x := range mails {
			if x.MessageID == m.MessageID {
				return
			}
		}
		mails = append(mails, m)
	})
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
	http.HandleFunc("GET /mail", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewEncoder(w).Encode(mails)
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

// serveSMTP accepts mail from anyone (a local stand-in, never exposed): enough
// of RFC 5321 for Go's net/smtp client, without extensions.
func serveSMTP(addr string, keep func(message)) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("webhook-sink mail on smtp://%s", addr)
	for {
		conn, err := l.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer conn.Close()
			tp := textproto.NewConn(conn)
			tp.PrintfLine("220 webhook-sink")
			var to string
			for {
				line, err := tp.ReadLine()
				if err != nil {
					return
				}
				switch strings.ToUpper(strings.Fields(line + " x")[0]) {
				case "EHLO", "HELO", "MAIL", "RSET", "NOOP":
					tp.PrintfLine("250 ok")
				case "RCPT":
					_, rcpt, _ := strings.Cut(line, ":")
					to = strings.Trim(strings.TrimSpace(rcpt), "<>")
					tp.PrintfLine("250 ok")
				case "DATA":
					tp.PrintfLine("354 end with <CRLF>.<CRLF>")
					raw, _ := tp.ReadDotBytes()
					m, err := mail.ReadMessage(bufio.NewReader(strings.NewReader(string(raw))))
					if err != nil {
						tp.PrintfLine("554 unreadable message")
						continue
					}
					text, _ := io.ReadAll(m.Body)
					subject, _ := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
					keep(message{MessageID: m.Header.Get("Message-ID"), To: to, From: m.Header.Get("From"), Subject: subject, Text: string(text)})
					tp.PrintfLine("250 kept")
				case "QUIT":
					tp.PrintfLine("221 bye")
					return
				default:
					tp.PrintfLine("502 not implemented")
				}
			}
		}()
	}
}
