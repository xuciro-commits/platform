package control

import (
	"cmp"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/mail"
	"os"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	SchemaInvite        = "platform.member.invite"
	SchemaInviteSend    = "platform.member.invite.send"
	SchemaInviteAccept  = "platform.member.invite.accept"
	SchemaInviteRevoke  = "platform.member.invite.revoke"
	SchemaJoin          = "platform.member.join"
	SchemaMemberSuspend = "platform.member.suspend"
	SchemaMemberResume  = "platform.member.resume"
	SchemaOffboard      = "platform.member.offboard"
	TokenType           = "platform.token"
	SchemaTokenIssue    = "platform.token.issue"
	SchemaTokenRevoke   = "platform.token.revoke"
	TokenPrefix         = "pat_"
)

type Token struct {
	ID     string    `json:"id"`
	Member string    `json:"member"`
	Label  string    `json:"label"`
	Change string    `json:"change"`
	Scopes []string  `json:"scopes,omitempty"`
	Until  string    `json:"until,omitempty"`
	Issued time.Time `json:"issued"`
}

const endedSessionsShown = 24 * time.Hour

type sessionTable struct {
	mu            sync.Mutex
	seen          map[string]map[string]*protocol.Session
	revoked       map[string]bool
	revokedBefore map[string]time.Time
}

func (st *sessionTable) end(member, id string, now time.Time) {
	if st.revoked == nil {
		st.revoked = map[string]bool{}
	}
	st.revoked[id] = true
	if s := st.seen[member][id]; s != nil && s.Ended.IsZero() {
		s.Ended = now
	}
}

func lifecycleActions() []platform.Action {
	admin := []string{Admin}
	reason := platform.Field{Name: "reason", Type: "string"}
	return []platform.Action{
		{Schema: SchemaInvite, Target: MemberType, Capability: "members", Title: "Invite member",
			Description: "Prepare an invitation for a verified email identity. It grants no access until sent and accepted.",
			Payload: []platform.Field{{Name: "subject", Type: "string", Required: true, Description: "user:<email> or client:<id>"},
				{Name: "agent", Type: "boolean", Description: "An AI agent"}}, Roles: admin},
		{Schema: SchemaInviteSend, Target: MemberType, Capability: "members", Title: "Send invitation", Description: "Send or renew an invitation after preparing its access.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaInviteAccept, Target: MemberType, Capability: "members", Title: "Accept invitation", Description: "Accept a pending invitation as its authenticated recipient.", Automation: true, Payload: []platform.Field{{Name: "subject", Type: "string", Required: true}}},
		{Schema: SchemaInviteRevoke, Target: MemberType, Capability: "members", Title: "Revoke invitation", Description: "Cancel an invitation before it is accepted.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaJoin, Target: MemberType, Capability: "members", Title: "Join by sign-in domain", Automation: true,
			Description: "A person whose mail domain the tenant lists under sign-in domains becomes a member on first sign-in, holding nothing until granted. Recorded by the host.",
			Payload:     []platform.Field{{Name: "subject", Type: "string", Required: true}}},
		{Schema: SchemaMemberSuspend, Target: MemberType, Capability: "members", Title: "Suspend member",
			Description: "The member keeps their roles on record but holds nothing and cannot act until resumed; their sessions end.", Payload: []platform.Field{reason}, Roles: admin},
		{Schema: SchemaMemberResume, Target: MemberType, Capability: "members", Title: "Resume member",
			Description: "A suspended member holds their roles again.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaOffboard, Target: MemberType, Capability: "members", Title: "Offboard member",
			Description: "The member leaves: their subjects and tokens go, their grants end, their record and history stay; their enterprise memberships end today and what they own passes to the successor, when named. Not reversible; add them again if they return.",
			Payload:     []platform.Field{reason, {Name: "successor", Type: "string", Description: "The member who takes over their delegations and open items"}}, Roles: admin},
		{Schema: SchemaTokenIssue, Target: TokenType, Capability: "account", Title: "Issue personal token",
			Description: "A credential for scripts and integrations that acts as you, within the permissions you name, until a day. The secret is shown once.",
			Payload: []platform.Field{{Name: "label", Type: "string", Required: true}, {Name: "scopes", Type: "json", Description: "Permission patterns, such as build.erpitem.* ; empty: everything you may do"},
				{Name: "until", Type: "string", Description: "Until this day, exclusive (YYYY-MM-DD)"}}, Roles: []string{platform.AnyMember}},
		{Schema: SchemaTokenRevoke, Target: TokenType, Capability: "account", Title: "Revoke personal token",
			Description: "The token stops working at once. Administrators revoke anyone's.", Payload: []platform.Field{}, Roles: []string{platform.AnyMember}},
	}
}

func (d *Console) decideLifecycle(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *platform.Error) {
	invalid := &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	var p struct {
		Subject, Reason, Successor string
		Agent                      bool
	}
	if json.Unmarshal(s.GetPayload(), &p) != nil {
		return nil, invalid
	}
	id := s.GetTarget().GetId()
	m := d.members[id]
	switch s.GetSchema().GetName() {
	case SchemaInvite, SchemaJoin:
		if !strings.HasPrefix(p.Subject, "user:") && !strings.HasPrefix(p.Subject, "client:") {
			return nil, invalid
		}
		bound := d.members[d.subjects[p.Subject]]
		if m != nil || bound != nil && (s.GetSchema().GetName() == SchemaJoin || bound.Status != platform.MemberLeft) {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		if s.GetSchema().GetName() == SchemaJoin {
			if !c.Automation || !c.Replaying && !d.JoinsSubject(p.Subject) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{subject} is not of a sign-in domain of this tenant", p.Subject)
			}
			return func(*pb.ChangeRecord) {
				d.members[id] = &platform.Member{ID: id, Tenant: d.tenant, Roles: map[string]string{}}
				d.subjects[p.Subject] = id
			}, nil
		}
		email := strings.TrimPrefix(p.Subject, "user:")
		address, err := mail.ParseAddress(email)
		if !strings.HasPrefix(p.Subject, "user:") || err != nil || address.Address != email || p.Agent {
			return nil, invalid
		}
		return func(r *pb.ChangeRecord) {
			d.members[id] = &platform.Member{ID: id, Tenant: d.tenant, Roles: map[string]string{}, Status: platform.MemberInvited, Invitation: &platform.Invitation{State: "draft", InvitedBy: c.ID, Created: r.GetRecordedTime().AsTime()}}
			d.subjects[p.Subject] = id
		}, nil
	case SchemaInviteSend:
		if m == nil || m.Invitation == nil || m.Status != platform.MemberInvited || (m.Invitation.State != "draft" && m.Invitation.State != "pending") {
			return nil, invalid
		}
		return func(r *pb.ChangeRecord) {
			invitation := *m.Invitation
			invitation.State = "pending"
			invitation.Expires = r.GetRecordedTime().AsTime().Add(7 * 24 * time.Hour)
			m.Invitation = &invitation
			d.then = func() {
				org := cmp.Or(d.tenantDefault(SettingName), d.tenant)
				body := "Sign in with your invited email to accept the invitation to " + org + "."
				if origin := strings.TrimRight(os.Getenv("PLATFORM_PUBLIC_URL"), "/"); strings.HasPrefix(origin, "https://") || strings.HasPrefix(origin, "http://localhost:") {
					body += "\n\n" + origin + "/"
				}
				c.Notify(platform.Notification{Category: protocol.NotificationSystem, Title: "You are invited to " + org, Key: "invite:" + r.GetChangeId(), Body: body}, r.GetRecordedTime().AsTime(), platform.Recipient{Member: id})
			}
		}, nil
	case SchemaInviteAccept:
		if !c.Automation || m == nil || m.Status != platform.MemberInvited || m.Invitation == nil || m.Invitation.State != "pending" || d.subjects[p.Subject] != id || !now.Before(m.Invitation.Expires) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The invitation is not available.")
		}
		return func(r *pb.ChangeRecord) {
			invitation := *m.Invitation
			invitation.State = "accepted"
			invitation.Accepted = r.GetRecordedTime().AsTime()
			m.Invitation = &invitation
			m.Status = ""
		}, nil
	case SchemaInviteRevoke:
		if m == nil || m.Status != platform.MemberInvited || m.Invitation == nil || m.Invitation.State == "accepted" {
			return nil, invalid
		}
		return func(*pb.ChangeRecord) {
			invitation := *m.Invitation
			invitation.State = "revoked"
			m.Invitation = &invitation
			m.Status = platform.MemberLeft
			m.Grants = nil
			m.Roles = map[string]string{}
		}, nil
	}
	if m == nil {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if m.Status == platform.MemberLeft || id == c.ID && !c.Replaying {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} may not change their own standing, and one who left has none", c.ID)
	}
	switch s.GetSchema().GetName() {
	case SchemaMemberSuspend:
		if m.PendingInvitation() {
			return nil, invalid
		}
		if d.lastAdministrator(m) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A workspace must retain an administrator.")
		}
		return func(r *pb.ChangeRecord) {
			m.Status = platform.MemberSuspended
			d.endSessions(id, "", r.GetRecordedTime().AsTime())
		}, nil
	case SchemaMemberResume:
		if m.Status != platform.MemberSuspended || m.PendingInvitation() {
			return nil, invalid
		}
		return func(*pb.ChangeRecord) { m.Status = "" }, nil
	case SchemaOffboard:
		if d.lastAdministrator(m) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A workspace must retain an administrator.")
		}
		successor := d.members[p.Successor]
		if p.Successor != "" && (successor == nil || p.Successor == id || !d.currentMember(successor).Active()) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} cannot take over: not an active member", p.Successor)
		}
		return func(r *pb.ChangeRecord) {
			m.Status = platform.MemberLeft
			m.Grants, m.Roles = nil, map[string]string{}
			if !c.Staging() {
				maps.DeleteFunc(d.subjects, func(_, member string) bool { return member == id })
			}
			maps.DeleteFunc(d.tokens, func(_ string, t *Token) bool { return t.Member == id })
			d.endSessions(id, "", r.GetRecordedTime().AsTime())
			for _, other := range d.members {
				if other.ID == id {
					continue
				}
				d.migrateGrants(other)
				other.Grants = slices.DeleteFunc(other.Grants, func(g platform.Grant) bool { return g.By == id && successor == nil })
				for i := range other.Grants {
					if other.Grants[i].By == id {
						other.Grants[i].By = successor.ID
					}
				}
				d.deriveRoles(other, other.Today(r.GetRecordedTime().AsTime()))
			}
			d.then = func() { d.handOver(c, id, p.Successor, p.Reason, r.GetRecordedTime().AsTime()) }
		}, nil
	}
	return nil, invalid
}

func (d *Console) decideToken(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *platform.Error) {
	invalid := &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	id := s.GetTarget().GetId()
	if id == "" {
		return nil, invalid
	}
	if s.GetSchema().GetName() == SchemaTokenRevoke {
		t := d.tokens[id]
		if t == nil {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if t.Member != c.ID && !c.Holds(PlatformApp, Admin) && !c.Replaying {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return func(r *pb.ChangeRecord) {
			delete(d.tokens, id)
			d.endSessions(t.Member, id, r.GetRecordedTime().AsTime())
		}, nil
	}
	var p struct {
		Label  string
		Scopes []string
		Until  string
	}
	if json.Unmarshal(s.GetPayload(), &p) != nil || p.Label == "" || d.tokens[id] != nil || d.members[c.ID] == nil {
		return nil, invalid
	}
	if _, err := time.Parse(time.DateOnly, p.Until); p.Until != "" && err != nil {
		return nil, invalid
	}
	if c.Scopes != nil && !c.Replaying {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if !c.Replaying && !HasTokenKey() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Personal tokens require a host token signing key")
	}
	return func(r *pb.ChangeRecord) {
		d.tokens[id] = &Token{ID: id, Member: c.ID, Label: p.Label, Change: r.GetChangeId(), Scopes: p.Scopes, Until: p.Until, Issued: r.GetRecordedTime().AsTime()}
	}, nil
}

var (
	tokenKeyMu sync.RWMutex
	tokenKey   []byte
)

func UseTokenKey(key []byte) {
	tokenKeyMu.Lock()
	defer tokenKeyMu.Unlock()
	if len(key) < 32 {
		tokenKey = nil
		return
	}
	tokenKey = append([]byte("platform-personal-token-v1|"), key...)
}

func HasTokenKey() bool {
	tokenKeyMu.RLock()
	defer tokenKeyMu.RUnlock()
	return len(tokenKey) > 0
}

func tokenSecret(tenant, id, change string) string {
	tokenKeyMu.RLock()
	defer tokenKeyMu.RUnlock()
	if len(tokenKey) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, tokenKey)
	mac.Write([]byte(tenant + "/" + id + "/" + change))
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:24])
}

func tokenHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

const mintWindow = 10 * time.Minute

func (d *Console) CloseTokenSecretWindows() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for id := range d.tokens {
		d.minted[id] = true
	}
}

func (d *Console) Minted(id, member string, now time.Time) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t := d.tokens[id]
	if t == nil || t.Member != member || d.minted[id] || now.Sub(t.Issued) > mintWindow {
		return "", false
	}
	secret := tokenSecret(d.tenant, id, t.Change)
	if secret == "" {
		return "", false
	}
	d.minted[id] = true
	return secret, true
}

func (d *Console) MemberByToken(secret string, now time.Time) (platform.Member, bool) {
	if !strings.HasPrefix(secret, TokenPrefix) || !HasTokenKey() {
		return platform.Member{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, t := range d.tokens {
		expected := tokenSecret(d.tenant, t.ID, t.Change)
		if expected == "" || !hmac.Equal([]byte(expected), []byte(secret)) {
			continue
		}
		m := d.members[t.Member]
		if m == nil || t.Until != "" && now.UTC().Format(time.DateOnly) >= t.Until {
			return platform.Member{}, false
		}
		if now.Sub(d.lastSeen["token:"+t.ID]) >= time.Minute {
			d.rememberSeen("token:"+t.ID, now)
		}
		out := d.currentMember(m)
		out.Scopes = t.Scopes
		if out.Scopes == nil {
			out.Scopes = []string{"*"}
		}
		return out, true
	}
	return platform.Member{}, false
}

func (d *Console) Tokens(member string, now time.Time) []protocol.TokenView {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []protocol.TokenView{}
	for _, id := range slices.Sorted(maps.Keys(d.tokens)) {
		t := d.tokens[id]
		if t.Member != member {
			continue
		}
		scopes := t.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		out = append(out, protocol.TokenView{ID: t.ID, Label: t.Label, Scopes: scopes, Until: t.Until, Issued: t.Issued, LastUsed: d.lastSeen["token:"+t.ID],
			Expired: t.Until != "" && now.UTC().Format(time.DateOnly) >= t.Until})
	}
	return out
}

func sessionID(credential string) string { return tokenHash(credential)[:16] }

func jwtIssuedAt(credential string) time.Time {
	parts := strings.Split(credential, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		IssuedAt   int64 `json:"iat"`
		IssuedAtUs int64 `json:"iat_us"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return time.Time{}
	}
	if claims.IssuedAtUs > 0 {
		return time.UnixMicro(claims.IssuedAtUs).UTC()
	}
	if claims.IssuedAt <= 0 {
		return time.Time{}
	}
	return time.Unix(claims.IssuedAt, 0).UTC()
}

func (d *Console) NoticeSession(member, credential, agent string, kind string, now time.Time) bool {
	id := sessionID(credential)
	hours := 0
	if d.host != nil {
		hours, _ = strconv.Atoi(d.tenantDefault(SettingSessionHours))
	}
	token := ""
	var tokenIssued time.Time
	if kind == "token" {
		d.mu.Lock()
		for key, t := range d.tokens {
			expected := tokenSecret(d.tenant, t.ID, t.Change)
			if expected != "" && t.Member == member && hmac.Equal([]byte(expected), []byte(credential)) {
				token = key
				tokenIssued = t.Issued
				break
			}
		}
		d.mu.Unlock()
	}
	d.sessions.mu.Lock()
	defer d.sessions.mu.Unlock()
	if d.sessions.revoked[id] {
		return false
	}
	if d.sessions.seen[member] == nil {
		d.sessions.seen[member] = map[string]*protocol.Session{}
	}
	s := d.sessions.seen[member][id]
	if cutoff, ok := d.sessions.revokedBefore[member]; ok {
		if s != nil && !s.First.After(cutoff) {
			d.sessions.end(member, id, now)
			return false
		}
		if kind == "sign-in" {
			if iat := jwtIssuedAt(credential); !iat.IsZero() && !iat.After(cutoff) {
				return false
			}
		}
		if kind == "token" && !tokenIssued.IsZero() && !tokenIssued.After(cutoff) {
			return false
		}
	}
	if s != nil && (kind == "sign-in" || kind == "dev") && hours > 0 && now.Sub(s.First) >= time.Duration(hours)*time.Hour {
		d.sessions.end(member, id, now)
		return false
	}
	if s == nil {
		s = &protocol.Session{ID: id, Kind: kind, Token: token, First: now, Agent: summarize(agent)}
		d.sessions.seen[member][id] = s
	}
	s.Last = now
	return true
}

func (d *Console) endSessions(member, token string, now time.Time) {
	d.sessions.mu.Lock()
	defer d.sessions.mu.Unlock()
	for id, s := range d.sessions.seen[member] {
		if (token == "" || s.Token == token) && s.Ended.IsZero() {
			d.sessions.end(member, id, now)
		}
	}
}

func (d *Console) EndSession(member, id, credential string, now time.Time) bool {
	cur := sessionID(credential)
	if id == "" || id == "current" {
		id = cur
	}
	d.sessions.mu.Lock()
	defer d.sessions.mu.Unlock()
	s := d.sessions.seen[member][id]
	if s == nil && id != cur {
		return false
	}
	d.sessions.end(member, id, now)
	return true
}

func (d *Console) EndOtherSessions(member, credential string, now time.Time) int {
	keep := sessionID(credential)
	d.sessions.mu.Lock()
	defer d.sessions.mu.Unlock()
	n := 0
	for id, s := range d.sessions.seen[member] {
		if id != keep && s.Ended.IsZero() {
			d.sessions.end(member, id, now)
			n++
		}
	}
	return n
}

func (d *Console) EndAllSessions(member, credential string, now time.Time) int {
	d.sessions.mu.Lock()
	defer d.sessions.mu.Unlock()
	if d.sessions.revokedBefore == nil {
		d.sessions.revokedBefore = map[string]time.Time{}
	}
	d.sessions.revokedBefore[member] = now
	n := 0
	for id, s := range d.sessions.seen[member] {
		if s.Ended.IsZero() {
			d.sessions.end(member, id, now)
			n++
		}
	}
	if credential != "" {
		cur := sessionID(credential)
		if !d.sessions.revoked[cur] {
			d.sessions.end(member, cur, now)
			n++
		}
	}
	return n
}

func (d *Console) Sessions(member, credential string, now time.Time) []protocol.Session {
	cur := sessionID(credential)
	d.sessions.mu.Lock()
	defer d.sessions.mu.Unlock()
	out := []protocol.Session{}
	for id, s := range d.sessions.seen[member] {
		if !s.Ended.IsZero() && now.Sub(s.Ended) > endedSessionsShown {
			delete(d.sessions.seen[member], id)
			continue
		}
		v := *s
		v.Current = v.ID == cur
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b protocol.Session) int {
		if (a.Ended.IsZero()) != (b.Ended.IsZero()) {
			if a.Ended.IsZero() {
				return -1
			}
			return 1
		}
		return b.Last.Compare(a.Last)
	})
	return out
}

func summarize(ua string) string {
	for _, name := range []string{"Edg/", "OPR/", "Firefox/", "Chrome/", "Safari/", "curl/", "Go-http-client/"} {
		if i := strings.Index(ua, name); i >= 0 {
			rest := ua[i:]
			if j := strings.IndexAny(rest, " ;)"); j > 0 {
				rest = rest[:j]
			}
			return strings.TrimSuffix(strings.Replace(rest, "/", " ", 1), ".0")
		}
	}
	if len(ua) > 40 {
		return ua[:40]
	}
	return ua
}

func (d *Console) JoinsSubject(subject string) bool {
	if d.host == nil {
		return false
	}
	email, ok := strings.CutPrefix(subject, "user:")
	_, domain, found := strings.Cut(email, "@")
	if !ok || !found || domain == "" {
		return false
	}
	for _, allowed := range strings.Split(d.tenantDefault(SettingDomains), ",") {
		if allowed = strings.ToLower(strings.TrimSpace(allowed)); allowed != "" && allowed == strings.ToLower(domain) {
			return true
		}
	}
	return false
}

func (d *Console) SecondFactorRequired() bool {
	return d.host != nil && d.tenantDefault(SettingMFA) == "true"
}

func (d *Console) JoinID(subject string) string {
	local, _, _ := strings.Cut(strings.TrimPrefix(subject, "user:"), "@")
	base := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return '-'
	}, local)
	base = strings.Trim(base, "-")
	if base == "" {
		base = "member"
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	id := base
	for n := 2; d.members[id] != nil; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	return id
}

func (d *Console) handOver(c platform.Caller, left, successor, reason string, now time.Time) {
	if successor == "" {
		return
	}
	c.Notify(platform.Notification{Category: protocol.NotificationAssignment, Title: "You take over from " + left, Key: "handover:" + left, Ref: MemberType + "/" + left,
		Body: strings.TrimSpace("Delegations " + left + " gave now stand in your name; reassign their open work and approvals to yourself from each app. " + reason)},
		now, platform.Recipient{Member: successor})
}

func (d *Console) JoinableSubject(subject string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.subjects[subject] == ""
}

type Specs struct {
	mu   sync.Mutex
	file string
	list []protocol.TenantSpec
}

func (s *Specs) Load(file string, development protocol.TenantSpec) error {
	s.file = file
	if file == "" {
		s.list = []protocol.TenantSpec{development}
		return nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var list []protocol.TenantSpec
	if err := json.Unmarshal(raw, &list); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	seen := map[string]bool{}
	for _, t := range list {
		if !ValidTenantID(t.ID) || seen[t.ID] {
			return fmt.Errorf("%s: tenant id %q is invalid or repeated", file, t.ID)
		}
		seen[t.ID] = true
	}
	s.list = list
	return nil
}

func (s *Specs) Get(id string) (protocol.TenantSpec, bool) {
	if s == nil {
		return protocol.TenantSpec{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.list {
		if t.ID == id {
			return t, true
		}
	}
	return protocol.TenantSpec{}, false
}

func (s *Specs) All() []protocol.TenantSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.list)
}

func (s *Specs) Add(spec protocol.TenantSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.ContainsFunc(s.list, func(t protocol.TenantSpec) bool { return t.ID == spec.ID }) {
		return fmt.Errorf("tenant %s exists", spec.ID)
	}
	next := append(slices.Clone(s.list), spec)
	if s.file != "" {
		raw, _ := json.MarshalIndent(next, "", "  ")
		if err := os.WriteFile(s.file, append(raw, '\n'), 0o644); err != nil {
			return err
		}
	}
	s.list = next
	return nil
}

var tenantID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

func ValidTenantID(id string) bool { return tenantID.MatchString(id) }
func NewSpecs(file string) *Specs  { return &Specs{file: file} }

type Registry[T comparable] struct {
	id          func(T) string
	unavailable func(T) bool
	mu          sync.RWMutex
	tenants     []T
}

func NewRegistry[T comparable](tenants []T, id func(T) string, unavailable func(T) bool) *Registry[T] {
	return &Registry[T]{tenants: append([]T(nil), tenants...), id: id, unavailable: unavailable}
}

func (r *Registry[T]) List() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]T(nil), r.tenants...)
}

func (r *Registry[T]) Add(t T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tenants = append(r.tenants, t)
}

func (r *Registry[T]) Current(id string) T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.tenants {
		if r.id(t) == id {
			return t
		}
	}
	var empty T
	return empty
}

func (r *Registry[T]) Replace(id string, old, fresh T) error {
	var empty T
	if fresh == empty || r.id(fresh) != id || r.unavailable(fresh) || old == empty || !r.unavailable(old) {
		return fmt.Errorf("tenant %s: recovery candidate or predecessor is not eligible", id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, t := range r.tenants {
		if r.id(t) == id {
			if t != old {
				return fmt.Errorf("tenant %s: another recovery already replaced this generation", id)
			}
			r.tenants[i] = fresh
			return nil
		}
	}
	return fmt.Errorf("tenant %s: not hosted", id)
}

func (d *Console) lastAdministrator(member *platform.Member) bool {
	if !d.currentMember(member).Holds(PlatformApp, Admin) {
		return false
	}
	for id, other := range d.members {
		if id != member.ID && d.currentMember(other).Holds(PlatformApp, Admin) {
			return false
		}
	}
	return true
}

func RequireEndedSession(ok bool) *platform.Error {
	if !ok {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return nil
}
