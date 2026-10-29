package platformserver

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"net/textproto"
	"net/url"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

type letter struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

func (t *Tenant) mailNotice(n platform.Notification, to platform.Reach) {
	for _, planned := range t.planMailNotice(n, to) {
		t.outbound = append(t.outbound, &effect{Effect: planned})
	}
}

func (t *Tenant) planMailNotice(n platform.Notification, to platform.Reach) []platform.Effect {
	var effects []platform.Effect
	address, lang := to.Address, to.Language
	if address == "" || !to.Mail {
		return effects
	}
	due := to.Due(n.At)
	for _, ep := range t.endpoints {
		if ep.Kind != "email" || !slices.Contains(ep.Notifications, n.App) {
			continue
		}
		text := t.i18n.Say(lang, n.Body)
		if n.Ref != "" {
			text += "\n\n" + t.i18n.Say(lang, "About") + ": " + n.Ref
		}
		body, _ := json.Marshal(letter{To: address, Subject: t.i18n.Say(lang, n.Title), Text: strings.TrimSpace(text) + "\n\n— " + t.ID + " · " + n.App})
		effects = append(effects, platform.Effect{ID: fmt.Sprintf("%s:notice:%s:%s", t.ID, n.ID, ep.ID), Endpoint: ep.ID,
			Event: n.App + "/notification", Target: n.Ref, At: n.At, State: "pending", Due: due, Body: string(body)})
	}
	return effects
}

func (t *Tenant) sendMail(ep Endpoint, x platform.Effect, now time.Time) platform.Outcome {
	sum := sha256.Sum256([]byte(x.Body))
	out := platform.Outcome{Effect: x.ID, Digest: hex.EncodeToString(sum[:])}
	var m letter
	u, err := url.Parse(ep.URL)
	if err != nil || u.Scheme != "smtp" || json.Unmarshal([]byte(x.Body), &m) != nil {
		out.Result, out.Detail = "rejected", "not an smtp:// endpoint or not a mail"
		return out
	}
	var auth smtp.Auth
	if u.User != nil {
		secret, ok := t.credentialValue(ep.Secret, "endpoint/"+ep.ID, ep.URL)
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
	case errors.Is(err, errPrivate):
		out.Result, out.Detail = "rejected", errPrivate.Error()
	case errors.As(err, &refused) && refused.Code >= 500:
		out.Result, out.Detail = "rejected", err.Error()
	default:
		out.Result, out.Detail = "retry", "no answer or a temporary refusal (resent with the same Message-ID): "+err.Error()
	}
	return out
}

func deliverMail(addr string, allowPrivate bool, auth smtp.Auth, from, to, msg string) error {
	conn, err := guardedDialer(allowPrivate).Dial("tcp", addr)
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
