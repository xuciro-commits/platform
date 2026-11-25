// Package records reads and writes the platform's call records for the voice gateway,
// only through the platform's public HTTP interface: submissions for every write,
// record reads, and the change stream to learn when to read again.
package records

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"platformvoice/gateway"
)

const (
	callType    = "phone.call"
	trunkType   = "phone.trunk"
	contactType = "party.contact"
	answerLimit = 4 << 20
)

// Client is the gateway's seat on the platform. It signs in with a token the tenant
// issued to the gateway's member, who holds the phone app's gateway role.
type Client struct {
	base    string
	tenant  string
	token   func() (string, error)
	http    *http.Client
	stream  *http.Client
	log     *slog.Logger
	changes chan struct{}
	lines   chan struct{}
	owners  []Owner

	mu        sync.Mutex
	principal string
	authority map[string]string
	versions  map[string]string
}

func New(base, tenant string, token func() (string, error), log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	return &Client{base: strings.TrimRight(base, "/"), tenant: tenant, token: token, log: log,
		http: &http.Client{Timeout: 15 * time.Second}, stream: &http.Client{}, changes: make(chan struct{}, 1), lines: make(chan struct{}, 1)}
}

// Changes signals each time the platform reports a change to call records.
func (c *Client) Changes() <-chan struct{} { return c.changes }

// Follow keeps the change stream open until ctx ends, reconnecting after a break.
func (c *Client) Follow(ctx context.Context) {
	for ctx.Err() == nil {
		err := c.follow(ctx)
		if ctx.Err() != nil {
			return
		}
		c.log.Warn("platform change stream broke; reconnecting", "error", err)
		c.notify()
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *Client) follow(ctx context.Context) error {
	req, err := c.request(ctx, http.MethodGet, "/v1/changes", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	res, err := c.stream.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return refusal(res)
	}
	scan := bufio.NewScanner(res.Body)
	scan.Buffer(make([]byte, 64<<10), 1<<20)
	event := ""
	for scan.Scan() {
		line := scan.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:") && event == "changed":
			var payload struct {
				Types []string `json:"types"`
			}
			json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &payload)
			if len(payload.Types) == 0 || contains(payload.Types, callType) {
				c.notify()
			}
		case line == "":
			event = ""
		}
	}
	if err := scan.Err(); err != nil {
		return err
	}
	return io.EOF
}

func (c *Client) notify() {
	signal(c.changes)
	signal(c.lines)
}

func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// LineChanges signals each time the platform reports a change to phone lines.
func (c *Client) LineChanges() <-chan struct{} { return c.lines }

// Lines reads the phone lines kept on the platform that are in use: each account's
// password, and the dialing settings of the lines that have any.
func (c *Client) Lines(ctx context.Context) (map[string]string, map[string]gateway.TrunkDialing, error) {
	var rows []struct {
		Account      string `json:"account"`
		Password     string `json:"password"`
		Active       *bool  `json:"active"`
		NumberHeader string `json:"numberheader"`
		Headers      string `json:"headers"`
	}
	if err := c.list(ctx, trunkType, [][]any{}, &rows); err != nil {
		return nil, nil, err
	}
	trunks, dialing := map[string]string{}, map[string]gateway.TrunkDialing{}
	for _, r := range rows {
		if r.Active != nil && !*r.Active || r.Account == "" || r.Password == "" {
			continue
		}
		trunks[r.Account] = r.Password
		d := gateway.TrunkDialing{NumberHeader: strings.TrimSpace(r.NumberHeader)}
		for _, line := range strings.Split(r.Headers, "\n") {
			name, value, ok := strings.Cut(line, ":")
			if name = strings.TrimSpace(name); ok && name != "" {
				if d.Headers == nil {
					d.Headers = map[string]string{}
				}
				d.Headers[name] = strings.TrimSpace(value)
			}
		}
		if d.NumberHeader != "" || len(d.Headers) > 0 {
			dialing[r.Account] = d
		}
	}
	return trunks, dialing, nil
}

// Submit runs a phone.call action through the platform's one write path. It answers
// the record's ID, which the platform names when a new record is created.
func (c *Client) Submit(ctx context.Context, action, id string, payload map[string]any, key string) (string, error) {
	principal, authority, version, err := c.describe(ctx, action)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"tenantId": c.tenant, "principalId": principal, "authority": authority,
		"target": map[string]string{"type": callType, "id": id},
		"schema": map[string]any{"name": action, "version": 1}, "idempotencyKey": key,
		"payload": base64.StdEncoding.EncodeToString(raw),
	}
	req, err := c.request(ctx, http.MethodPost, "/v1/submissions", body)
	if err != nil {
		return "", err
	}
	if version != "" {
		req.Header.Set("Platform-Schema-Version", version)
	}
	var answer struct {
		Record *struct {
			Submission struct {
				Target struct {
					ID string `json:"id"`
				} `json:"target"`
			} `json:"submission"`
		} `json:"record"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := c.do(req, &answer); err != nil {
		return "", err
	}
	if answer.Record == nil {
		refused := &gateway.Refused{Status: http.StatusOK}
		if answer.Error != nil {
			refused.Code, refused.Message = answer.Error.Code, answer.Error.Message
		}
		return "", refused
	}
	if got := answer.Record.Submission.Target.ID; got != "" {
		return got, nil
	}
	return id, nil
}

type callRow struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	Direction string    `json:"direction"`
	Number    string    `json:"number"`
	Agent     string    `json:"agent"`
	Offer     string    `json:"offer"`
	Answer    string    `json:"answer"`
	Ended     bool      `json:"ended"`
	Preferred string    `json:"preferred"`
	Widened   bool      `json:"widened"`
	CalledAt  time.Time `json:"calledat"`
}

func (r callRow) record() gateway.Record {
	return gateway.Record{ID: r.ID, State: r.State, Direction: r.Direction, Number: r.Number, Agent: r.Agent,
		Offer: r.Offer, Answer: r.Answer, Ended: r.Ended, Preferred: r.Preferred, Widened: r.Widened, CalledAt: r.CalledAt}
}

func (c *Client) Read(ctx context.Context, id string) (gateway.Record, error) {
	req, err := c.request(ctx, http.MethodGet, "/v1/records/"+callType+"/"+url.PathEscape(id), nil)
	if err != nil {
		return gateway.Record{}, err
	}
	var view struct {
		Record callRow `json:"record"`
	}
	if err := c.do(req, &view); err != nil {
		return gateway.Record{}, err
	}
	return view.Record.record(), nil
}

// Active lists the calls still ringing or in progress.
func (c *Client) Active(ctx context.Context) ([]gateway.Record, error) {
	var rows []callRow
	if err := c.list(ctx, callType, [][]any{{"state", "in", []string{"ringing", "connected"}}}, &rows); err != nil {
		return nil, err
	}
	out := make([]gateway.Record, len(rows))
	for i, r := range rows {
		out[i] = r.record()
	}
	return out, nil
}

// Owner names a record type that gives contacts an owner: records of Type whose
// Contact field refers to the party contact, with the owning member in Owner.
type Owner struct {
	Type    string `json:"type"`
	Contact string `json:"contact"`
	Owner   string `json:"owner"`
}

// OwnedBy sets where the client looks for a contact's owner, in order.
func (c *Client) OwnedBy(owners []Owner) { c.owners = owners }

// Contact finds the business partner contact a number belongs to: the first one with
// it as mobile or phone who has not asked not to be contacted, and the member who owns
// that contact in the first owner source that has one.
func (c *Client) Contact(ctx context.Context, number string) gateway.Party {
	contact, organization := c.party(ctx, number)
	if contact == "" {
		return gateway.Party{}
	}
	return gateway.Party{Contact: contact, Organization: organization, Owner: c.owner(ctx, contact)}
}

func (c *Client) owner(ctx context.Context, contact string) string {
	for _, o := range c.owners {
		var rows []map[string]any
		if err := c.list(ctx, o.Type, [][]any{{o.Contact, "=", contact}}, &rows); err != nil {
			c.log.Warn("could not look up the contact's owner", "type", o.Type, "error", err)
			continue
		}
		for _, row := range rows {
			if owner, _ := row[o.Owner].(string); owner != "" {
				return owner
			}
		}
	}
	return ""
}

func (c *Client) party(ctx context.Context, number string) (string, string) {
	if number == "" {
		return "", ""
	}
	for _, field := range []string{"mobile", "phone"} {
		var rows []struct {
			ID           string `json:"id"`
			Organization string `json:"organization"`
			DoNotContact bool   `json:"donotcontact"`
		}
		if err := c.list(ctx, contactType, [][]any{{field, "=", number}}, &rows); err != nil {
			c.log.Warn("could not look up the caller", "error", err)
			return "", ""
		}
		for _, r := range rows {
			if !r.DoNotContact {
				return r.ID, r.Organization
			}
		}
	}
	return "", ""
}

func (c *Client) list(ctx context.Context, typ string, domain [][]any, rows any) error {
	encoded, err := json.Marshal(domain)
	if err != nil {
		return err
	}
	req, err := c.request(ctx, http.MethodGet, "/v1/records/"+typ+"?limit=200&domain="+url.QueryEscape(string(encoded)), nil)
	if err != nil {
		return err
	}
	var page struct {
		Records json.RawMessage `json:"records"`
	}
	if err := c.do(req, &page); err != nil {
		return err
	}
	return json.Unmarshal(page.Records, rows)
}

// describe learns, once, who the gateway is on the platform, which authority owns
// call records, and the schema version of each action.
func (c *Client) describe(ctx context.Context, action string) (string, string, string, error) {
	c.mu.Lock()
	known := c.principal != ""
	c.mu.Unlock()
	if !known {
		if err := c.load(ctx); err != nil {
			return "", "", "", err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	authority := c.authority[callType]
	if authority == "" {
		return "", "", "", errors.New("the platform has no phone call records; install the phone app")
	}
	return c.principal, authority, c.versions[action], nil
}

func (c *Client) load(ctx context.Context) error {
	var me struct {
		PrincipalID string `json:"principalId"`
	}
	var declarations []struct {
		DataClass   string `json:"dataClass"`
		AuthorityID string `json:"authorityId"`
	}
	var actions []struct {
		Schema        string `json:"schema"`
		SchemaVersion *struct {
			Version string `json:"version"`
		} `json:"schemaVersion"`
	}
	for path, into := range map[string]any{"/v1/me": &me, "/v1/declarations": &declarations, "/v1/actions": &actions} {
		req, err := c.request(ctx, http.MethodGet, path, nil)
		if err != nil {
			return err
		}
		if err := c.do(req, into); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	if me.PrincipalID == "" {
		return errors.New("the platform did not say who the gateway is")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.principal, c.authority, c.versions = me.PrincipalID, map[string]string{}, map[string]string{}
	for _, d := range declarations {
		c.authority[d.DataClass] = d.AuthorityID
	}
	for _, a := range actions {
		if a.SchemaVersion != nil {
			c.versions[a.Schema] = a.SchemaVersion.Version
		}
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, path string, body any) (*http.Request, error) {
	token, err := c.token()
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Platform-Tenant", c.tenant)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) do(req *http.Request, into any) error {
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusUnauthorized {
			c.mu.Lock()
			c.principal = ""
			c.mu.Unlock()
		}
		return refusal(res)
	}
	return json.NewDecoder(io.LimitReader(res.Body, answerLimit)).Decode(into)
}

func refusal(res *http.Response) error {
	var body struct {
		Error json.RawMessage `json:"error"`
	}
	json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body)
	refused := &gateway.Refused{Status: res.StatusCode}
	var detail struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body.Error, &detail) == nil {
		refused.Code, refused.Message = detail.Code, detail.Message
	} else {
		json.Unmarshal(body.Error, &refused.Message)
	}
	if res.StatusCode >= 500 {
		return fmt.Errorf("the platform answered %d: %s", res.StatusCode, refused.Message)
	}
	return refused
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
