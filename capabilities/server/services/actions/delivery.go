package actions

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/textproto"
	"net/url"
	"platformserver/platform"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Letter struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

const effectTimeout = 10 * time.Second

type Credentials interface {
	CredentialValue(name, consumer, address string) ([]byte, bool)
	CredentialHeader(name, consumer, address string) ([]byte, bool)
}

type Transport struct {
	Credentials Credentials
	Send        func(req *http.Request, allowPrivate bool) (*http.Response, error)
}

type Endpoint struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	URL           string   `json:"url"`
	Secret        string   `json:"secret,omitempty"`
	Events        []string `json:"events,omitempty"`
	Effects       []string `json:"effects,omitempty"`
	From          string   `json:"from,omitempty"`
	Notifications []string `json:"notifications,omitempty"`
	AllowPrivate  bool     `json:"allowPrivate,omitempty"`
	Irreversible  bool     `json:"irreversible,omitempty"`
}

var ErrPrivate = fmt.Errorf("private address refused")

func GuardedDialer(allowPrivate bool) *net.Dialer {
	return &net.Dialer{Timeout: effectTimeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, _ := net.SplitHostPort(address)
		ip := net.ParseIP(host)
		if !allowPrivate && (ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
			return ErrPrivate
		}
		return nil
	}}
}

func (d Transport) Webhook(ep Endpoint, x platform.Effect, now time.Time) platform.Outcome {
	sum := sha256.Sum256([]byte(x.Body))
	out := platform.Outcome{Effect: x.ID, Digest: hex.EncodeToString(sum[:])}
	secret, ok := d.Credentials.CredentialValue(ep.Secret, "endpoint/"+ep.ID, ep.URL)
	if !ok {
		out.Result, out.Detail = "retry", "secret "+ep.Secret+" missing"
		return out
	}
	u, err := url.Parse(ep.URL)
	if err != nil || u.Scheme != "https" && !(u.Scheme == "http" && ep.AllowPrivate) {
		out.Result, out.Detail = "rejected", "only https endpoints (http when private addresses are allowed)"
		return out
	}
	stamp := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(x.ID + "." + stamp + "." + x.Body))
	req, _ := http.NewRequest(http.MethodPost, ep.URL, strings.NewReader(x.Body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("webhook-id", x.ID)
	req.Header.Set("webhook-timestamp", stamp)
	req.Header.Set("webhook-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Idempotency-Key", x.ID)
	resp, err := d.Send(req, ep.AllowPrivate)
	switch {
	case err != nil && strings.Contains(err.Error(), ErrPrivate.Error()):
		out.Result, out.Detail = "rejected", ErrPrivate.Error()
	case err != nil:
		out.Result, out.Detail = "unknown", "No delivery answer; reconcile or record a human decision: "+err.Error()
	default:
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if x.App != "" && json.Valid(answer) {
			out.Answer = answer
		}
		code := resp.StatusCode
		switch {
		case code >= 200 && code < 300:
			out.Result = "delivered"
		case code >= 400 && code < 500 && code != 408 && code != 429:
			out.Result, out.Detail = "rejected", resp.Status
		default:
			out.Result, out.Detail = "retry", resp.Status
		}
	}
	return out
}

func (d Transport) Mail(ep Endpoint, x platform.Effect, now time.Time) platform.Outcome {
	sum := sha256.Sum256([]byte(x.Body))
	out := platform.Outcome{Effect: x.ID, Digest: hex.EncodeToString(sum[:])}
	var m Letter
	u, err := url.Parse(ep.URL)
	if err != nil || u.Scheme != "smtp" || json.Unmarshal([]byte(x.Body), &m) != nil {
		out.Result, out.Detail = "rejected", "not an smtp:// endpoint or not a mail"
		return out
	}
	var auth smtp.Auth
	if u.User != nil {
		secret, ok := d.Credentials.CredentialValue(ep.Secret, "endpoint/"+ep.ID, ep.URL)
		if !ok {
			out.Result, out.Detail = "retry", "secret "+ep.Secret+" missing"
			return out
		}
		auth = smtp.PlainAuth("", u.User.Username(), string(secret), u.Hostname())
	}
	addr := u.Host
	if u.Port() == "" {
		addr = net.JoinHostPort(u.Hostname(), "587")
	}
	id := strings.ReplaceAll(x.ID, ":", ".") + "@platform"
	var msg strings.Builder
	for _, h := range [][2]string{{"From", ep.From}, {"To", m.To}, {"Subject", mime.QEncoding.Encode("utf-8", m.Subject)},
		{"Date", now.Format(time.RFC1123Z)}, {"Message-ID", "<" + id + ">"}, {"MIME-Version", "1.0"},
		{"Content-Type", "text/plain; charset=utf-8"}, {"Content-Transfer-Encoding", "8bit"}, {"X-Platform-Effect", x.ID}} {
		msg.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	msg.WriteString("\r\n" + strings.ReplaceAll(strings.ReplaceAll(m.Text, "\r\n", "\n"), "\n", "\r\n") + "\r\n")
	err = deliverMail(addr, ep.AllowPrivate, auth, ep.From, m.To, msg.String())
	var refused *textproto.Error
	switch {
	case err == nil:
		out.Result = "delivered"
	case errors.Is(err, ErrPrivate):
		out.Result, out.Detail = "rejected", ErrPrivate.Error()
	case errors.As(err, &refused) && refused.Code >= 500:
		out.Result, out.Detail = "rejected", err.Error()
	default:
		out.Result, out.Detail = "retry", "no answer or a temporary refusal (resent with the same Message-ID): "+err.Error()
	}
	return out
}

func deliverMail(addr string, allowPrivate bool, auth smtp.Auth, from, to, msg string) error {
	conn, err := GuardedDialer(allowPrivate).Dial("tcp", addr)
	if err != nil {
		return err
	}
	conn.SetDeadline(time.Now().Add(effectTimeout))
	host, _, _ := net.SplitHostPort(addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
