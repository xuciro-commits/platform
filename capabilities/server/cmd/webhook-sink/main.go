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
//	GET  /v1/models, POST /v1/chat/completions   a local model server on the
//	                OpenAI wire (ADR-0015): model "echo" repeats the last message;
//	                given tools (an agent, ADR-0021), it calls a read tool first,
//	                then finishes proposing the first item read whose ID the goal
//	                does not name and whose product it does; the helpdesk's
//	                triage agent triages a ticket as normal and replies
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
	"regexp"
	"slices"
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
	http.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "echo", "name": "Echo", "context_length": 4096}}})
	})
	http.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string
			Messages []struct{ Role, Content string }
			Tools    []struct {
				Function struct{ Name string }
			}
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.Model != "echo" || len(req.Messages) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "model echo only"}})
			return
		}
		last := req.Messages[len(req.Messages)-1].Content
		if len(req.Tools) > 0 { // an agent (ADR-0021): read first, then propose the first ID read that the goal does not name
			call := func(name string, args map[string]string) {
				raw, _ := json.Marshal(args)
				json.NewEncoder(w).Encode(map[string]any{"model": "echo", "usage": map[string]int{"prompt_tokens": 50, "completion_tokens": 10},
					"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []map[string]any{
						{"id": "c1", "type": "function", "function": map[string]string{"name": name, "arguments": string(raw)}}}}}}})
			}
			// The helpdesk's triage agent: triage as normal, reply, finish.
			if slices.ContainsFunc(req.Tools, func(t struct{ Function struct{ Name string } }) bool {
				return t.Function.Name == "helpdesk_ticket_triage"
			}) {
				ticket := regexp.MustCompile(`ticket (\S+) from`).FindStringSubmatch(req.Messages[1].Content)
				done := 0
				for _, m := range req.Messages {
					if m.Role == "tool" {
						done++
					}
				}
				switch {
				case ticket == nil || done >= 2:
					call("finish", map[string]string{"result": "triaged and answered", "rationale": "Both steps are done."})
				case done == 0:
					call("helpdesk_ticket_triage", map[string]string{"target": ticket[1], "category": "other", "priority": "normal", "rationale": "Nothing marks it urgent."})
				default:
					call("helpdesk_ticket_reply", map[string]string{"target": ticket[1], "reply": "Thank you for writing. A colleague will follow up today.", "rationale": "Acknowledge and hand it on."})
				}
				return
			}
			if req.Messages[len(req.Messages)-1].Role != "tool" {
				for _, t := range req.Tools {
					if strings.HasPrefix(t.Function.Name, "read_") {
						call(t.Function.Name, map[string]string{"rationale": "Read what is there before proposing anything."})
						return
					}
				}
			}
			goal, found := req.Messages[1].Content, ""
			var items []map[string]any // a list read: the first item whose product the goal names, and whose ID it does not
			json.Unmarshal([]byte(last), &items)
			for _, item := range items {
				id := regexp.MustCompile(`[A-Z]{2,}-\d+`).FindString(fmt.Sprint(item))
				if product, _ := item["product"].(string); id != "" && !strings.Contains(goal, id) && (product == "" || strings.Contains(goal, product)) {
					found = id
					break
				}
			}
			call("finish", map[string]string{"result": `{"planned":"` + found + `"}`, "rationale": "The first planned order read that no other order fulfils: " + cmpOr(found, "none") + "."})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "echo", "choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "echo: " + last}}},
			"usage": map[string]int{"prompt_tokens": len(strings.Fields(last)), "completion_tokens": len(strings.Fields(last)) + 1}})
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

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
