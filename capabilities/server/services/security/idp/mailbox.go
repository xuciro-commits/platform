package idp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"platformserver/kernel/protocol"
)

type LoopbackMailbox struct {
	listener net.Listener
	addr     string
	file     string
	mu       sync.Mutex
	messages []protocol.CapturedMail
	seq      int
	closed   bool
	wg       sync.WaitGroup
}

func StartLoopbackMailbox(dir string) (*LoopbackMailbox, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	m := &LoopbackMailbox{
		listener: ln,
		addr:     ln.Addr().String(),
	}
	if strings.TrimSpace(dir) != "" {
		m.file = filepath.Join(dir, "mailbox.json")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			_ = ln.Close()
			return nil, err
		}
		m.loadLocked()
	}
	m.wg.Add(1)
	go m.acceptLoop()
	return m, nil
}

func (m *LoopbackMailbox) Endpoint() protocol.MailChannel {
	return protocol.MailChannel{
		URL:          "smtp://" + m.addr,
		From:         "noreply@platform.local",
		AllowPrivate: true,
	}
}

func (m *LoopbackMailbox) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()
	err := m.listener.Close()
	m.wg.Wait()
	return err
}

func (m *LoopbackMailbox) Messages(to string) []protocol.CapturedMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadLocked()
	want := strings.ToLower(strings.TrimSpace(to))
	out := []protocol.CapturedMail{}
	for _, msg := range m.messages {
		if want == "" || strings.EqualFold(msg.To, want) {
			out = append(out, msg)
		}
	}
	return out
}

func (m *LoopbackMailbox) loadLocked() {
	if m.file == "" {
		return
	}
	raw, err := os.ReadFile(m.file)
	if err != nil || len(raw) == 0 {
		return
	}
	var saved []protocol.CapturedMail
	if json.Unmarshal(raw, &saved) == nil {
		m.messages = saved
		if len(saved) > m.seq {
			m.seq = len(saved)
		}
	}
}

func (m *LoopbackMailbox) appendMessage(from string, recipients []string, raw []byte) {
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	subject := ""
	body := string(raw)
	msgID := ""
	sentAt := time.Now().UTC()
	if err == nil {
		dec := new(mime.WordDecoder)
		subject, _ = dec.DecodeHeader(parsed.Header.Get("Subject"))
		msgID = strings.Trim(parsed.Header.Get("Message-ID"), "<> ")
		if d, parseErr := parsed.Header.Date(); parseErr == nil {
			sentAt = d.UTC()
		}
		if b, readErr := io.ReadAll(parsed.Body); readErr == nil {
			body = strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadLocked()
	for _, rcpt := range recipients {
		m.seq++
		id := msgID
		if id == "" {
			id = fmt.Sprintf("mail-%d", m.seq)
		}
		m.messages = append(m.messages, protocol.CapturedMail{
			ID:      id,
			From:    from,
			To:      strings.ToLower(strings.TrimSpace(rcpt)),
			Subject: subject,
			Text:    body,
			SentAt:  sentAt,
		})
	}
	if len(m.messages) > 500 {
		m.messages = slices.Clone(m.messages[len(m.messages)-500:])
	}
	if m.file != "" {
		if encoded, marshalErr := json.Marshal(m.messages); marshalErr == nil {
			_ = writeFileAtomic(m.file, encoded)
		}
	}
}

func (m *LoopbackMailbox) acceptLoop() {
	defer m.wg.Done()
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return
		}
		m.wg.Add(1)
		go func(c net.Conn) {
			defer m.wg.Done()
			m.serveConn(c)
		}(conn)
	}
}

func (m *LoopbackMailbox) serveConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	reply := func(line string) bool {
		_, err := w.WriteString(line + "\r\n")
		if err != nil {
			return false
		}
		return w.Flush() == nil
	}
	if !reply("220 localhost ESMTP") {
		return
	}
	from := ""
	var rcpts []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(upper, "EHLO "):
			if !reply("250-localhost\r\n250 8BITMIME") {
				return
			}
		case strings.HasPrefix(upper, "HELO "):
			if !reply("250 localhost") {
				return
			}
		case strings.HasPrefix(upper, "MAIL FROM:"):
			from = extractAngleAddr(trimmed[len("MAIL FROM:"):])
			rcpts = nil
			if !reply("250 2.1.0 OK") {
				return
			}
		case strings.HasPrefix(upper, "RCPT TO:"):
			rcpts = append(rcpts, extractAngleAddr(trimmed[len("RCPT TO:"):]))
			if !reply("250 2.1.5 OK") {
				return
			}
		case upper == "DATA":
			if !reply("354 End data with <CR><LF>.<CR><LF>") {
				return
			}
			var data bytes.Buffer
			for {
				dline, derr := r.ReadString('\n')
				if derr != nil {
					return
				}
				if dline == ".\r\n" || dline == ".\n" {
					break
				}
				if strings.HasPrefix(dline, "..") {
					dline = dline[1:]
				}
				data.WriteString(dline)
			}
			m.appendMessage(from, rcpts, data.Bytes())
			if !reply("250 2.0.0 OK") {
				return
			}
		case upper == "RSET":
			from, rcpts = "", nil
			if !reply("250 2.0.0 OK") {
				return
			}
		case upper == "NOOP":
			if !reply("250 2.0.0 OK") {
				return
			}
		case upper == "QUIT":
			_ = reply("221 2.0.0 Bye")
			return
		default:
			if !reply("500 5.5.1 Unrecognized command") {
				return
			}
		}
	}
}

func extractAngleAddr(raw string) string {
	s := strings.TrimSpace(raw)
	if start := strings.IndexByte(s, '<'); start >= 0 {
		if end := strings.IndexByte(s[start+1:], '>'); end >= 0 {
			return strings.TrimSpace(s[start+1 : start+1+end])
		}
	}
	return strings.Trim(s, "<> ")
}
