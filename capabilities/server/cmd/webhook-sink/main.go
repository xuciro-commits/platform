// Command webhook-sink is a webhook receiver for the local stack and its
// rehearsal: it checks Standard Webhooks signatures with the secret
// in WEBHOOK_SECRET, keeps one copy per webhook-id, and lists what it kept. It
// is also a mail server (SMTP on -smtp) that keeps one message per Message-ID.
//
//	POST /hook      a webhook; 204 when kept (or already kept), 401 on a bad signature
//	GET  /received  {"calls": n, "kept": {"<webhook-id>": <body>}}
//	POST /fail?on=true|false   answer 503 to every webhook until switched off
//	GET  /mail      [{"messageId", "to", "from", "subject", "text"}], oldest first
//	GET  /v1/models, POST /v1/chat/completions   a local model server on the
//	                OpenAI wire: model "echo" repeats the last message;
//	                given tools (an agent), it calls a read tool first,
//	                then finishes proposing the first item read whose ID the goal
//	                does not name and whose product it does;
//	                model "embed" embeds hashed words;
//	                the plant's planner asks the supplier's agent, then answers
//	                bounded record-advice requests get a fixed typed fixture
//	                model "review" returns fixed review/ask samples, not model quality
//	GET /messages, POST /observations   empty channel and fixed state-window samples
//	POST /a2a       a supplier's agent over A2A 1.0 (card at /a2a/.well-known/agent-card.json):
//	                every SendMessage gets a completed task with a lead time of 12 days
package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
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
	"time"
	"unicode"
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
		json.NewEncoder(w).Encode(map[string]any{"calls": calls, "kept": kept})
	})
	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})
	http.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "echo", "name": "Echo", "context_length": 4096}, {"id": "embed", "name": "Hashed words", "context_length": 8192}, {"id": "review", "name": "Fixed review fixture", "context_length": 4096}}})
	})
	http.HandleFunc("GET /messages", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"messages": []any{}})
	})
	http.HandleFunc("POST /observations", func(w http.ResponseWriter, r *http.Request) {
		var query struct {
			Mode         string `json:"mode"`
			Since, Until time.Time
		}
		if json.NewDecoder(r.Body).Decode(&query) != nil || query.Mode != "intervals" || query.Since.IsZero() || !query.Since.Before(query.Until) || query.Until.Sub(query.Since) > 5*time.Minute {
			http.Error(w, "fixed interval fixture supports windows up to five minutes", http.StatusBadRequest)
			return
		}
		points := []map[string]any{}
		for at := query.Since; at.Before(query.Until); at = at.Add(10 * time.Second) {
			value := "run"
			if offset := at.Sub(query.Since); offset >= 20*time.Second && offset < 100*time.Second {
				value = "down"
			}
			points = append(points, map[string]any{"at": at, "receivedAt": at, "quality": "good", "value": value})
		}
		json.NewEncoder(w).Encode(map[string]any{"intervals": map[string]any{"since": query.Since, "until": query.Until, "revision": "local-fixture-1",
			"coverage": map[string]any{"availableSince": query.Since, "completeUntil": query.Until, "good": len(points)}, "points": points}})
	})
	// A supplier's agent over A2A 1.0: it answers every SendMessage
	// with a completed task, the lead time of the product the message names.
	http.HandleFunc("GET /a2a/.well-known/agent-card.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"name": "Harbour Metals planner", "description": "Lead times of Harbour Metals' products.", "version": "1",
			"supportedInterfaces": []map[string]string{{"url": "http://" + r.Host + "/a2a", "protocolBinding": "JSONRPC", "protocolVersion": "1.0"}},
			"capabilities":        map[string]bool{"streaming": false}, "defaultInputModes": []string{"text/plain"}, "defaultOutputModes": []string{"application/json"},
			"skills": []map[string]any{{"id": "lead-time", "name": "Lead time", "description": "How many days until a product can be delivered.", "tags": []string{"supply"}}}})
	})
	supplierReply := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage
			Method string
			Params struct {
				Message struct {
					MessageID string
					Parts     []struct{ Text string }
				}
			}
		}
		json.NewDecoder(r.Body).Decode(&req)
		text := ""
		for _, p := range req.Params.Message.Parts {
			text += p.Text
		}
		product := cmpOr(regexp.MustCompile(`P-\d+`).FindString(text), "unknown")
		parts := []map[string]any{{"data": map[string]any{"supplier": "Harbour Metals", "product": product, "leadTimeDays": 12}}}
		if r.URL.Path == "/rpc" {
			parts = []map[string]any{{"text": "Local supplier fixture: delivery in 12 days; confirm the actual order separately."}}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"task": map[string]any{
			"id": "task-" + req.Params.Message.MessageID, "contextId": req.Params.Message.MessageID, "status": map[string]string{"state": "TASK_STATE_COMPLETED"},
			"artifacts": []map[string]any{{"artifactId": "lead-time", "name": "lead time",
				"parts": parts}}}}})
	}
	http.HandleFunc("POST /a2a", supplierReply)
	http.HandleFunc("POST /rpc", supplierReply)
	http.HandleFunc("POST /v1/embeddings", func(w http.ResponseWriter, r *http.Request) { // "embed": each word hashed into 64 dimensions
		var req struct{ Input []string }
		json.NewDecoder(r.Body).Decode(&req)
		data, tokens := []map[string]any{}, 0
		for i, text := range req.Input {
			v := make([]float32, 64)
			for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
				h := fnv.New32a()
				h.Write([]byte(word))
				v[h.Sum32()%64]++
				tokens++
			}
			data = append(data, map[string]any{"index": i, "embedding": v})
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "embed", "data": data, "usage": map[string]int{"prompt_tokens": tokens}})
	})
	http.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string
			Messages []struct{ Role, Content string }
			Tools    []struct {
				Function struct{ Name string }
			}
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.Model != "echo" && req.Model != "review" || len(req.Messages) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "model echo or review only"}})
			return
		}
		if req.Model == "review" {
			message := map[string]any{}
			if len(req.Tools) > 0 {
				args, _ := json.Marshal(map[string]any{"question": "Which confirmed record and facts should I review?", "answers": []string{"Provide the target"}, "rationale": "A confirmed target is required before a business change."})
				message["tool_calls"] = []map[string]any{{"id": "ask-target", "type": "function", "function": map[string]string{"name": "ask", "arguments": string(args)}}}
			} else {
				content, _ := json.Marshal(map[string]any{"summary": "Review the actual record and confirm missing facts with the responsible person.", "category": "review", "review": true, "confidence": 0.9})
				message["content"] = string(content)
			}
			json.NewEncoder(w).Encode(map[string]any{"model": "review", "choices": []map[string]any{{"message": message}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "cost": 0.001}})
			return
		}
		last := req.Messages[len(req.Messages)-1].Content
		// A deterministic fixture for the first typed function; it proves the
		// real wire, validation and recovery path, not model reasoning quality.
		if len(req.Tools) == 0 && len(req.Messages) == 2 && req.Messages[0].Role == "system" &&
			strings.HasPrefix(req.Messages[0].Content, "Summarise only the provided record.") && json.Valid([]byte(last)) {
			content, _ := json.Marshal(map[string]any{"summary": "Record: " + last, "category": "routine", "review": false})
			json.NewEncoder(w).Encode(map[string]any{"model": "echo", "choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": string(content)}}},
				"usage": map[string]any{"prompt_tokens": len(strings.Fields(last)), "completion_tokens": 20, "cost": 0.01}})
			return
		}
		if len(req.Tools) > 0 { // an agent: read first, then propose the first ID read that the goal does not name
			call := func(name string, args map[string]string) {
				raw, _ := json.Marshal(args)
				json.NewEncoder(w).Encode(map[string]any{"model": "echo", "usage": map[string]int{"prompt_tokens": 50, "completion_tokens": 10},
					"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "", "tool_calls": []map[string]any{
						{"id": "c1", "type": "function", "function": map[string]string{"name": name, "arguments": string(raw)}}}}}}})
			}
			// The plant's planner: ask the supplier's agent, then answer with what it said.
			if slices.ContainsFunc(req.Tools, func(t struct{ Function struct{ Name string } }) bool { return t.Function.Name == "emit_lead_time" }) {
				if req.Messages[len(req.Messages)-1].Role != "tool" {
					call("emit_lead_time", map[string]string{"message": req.Messages[1].Content, "rationale": "Only the supplier knows its lead time."})
					return
				}
				_, answer, _ := strings.Cut(last, "answered: ")
				call("finish", map[string]string{"result": cmpOr(answer, last), "rationale": "The supplier's agent answered."})
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
