package platformserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Member lifecycle, personal tokens and sessions (ADR-0079 §4–5). A member's
// standing is a decision of the directory: invited when added before they
// sign in, suspended (nothing is held until resumed), left (offboarded: the
// subjects go, the grants go, the record stays for history). A personal token
// is a credential the host issues for scripts and integrations, derived from a private key,
// bounded in time and scope; a session is the host's memory of a credential
// it saw, which a member may end.
const (
	SchemaInvite        = "platform.member.invite"
	SchemaMemberSuspend = "platform.member.suspend"
	SchemaMemberResume  = "platform.member.resume"
	SchemaOffboard      = "platform.member.offboard"
	TokenType           = "platform.token"
	SchemaTokenIssue    = "platform.token.issue"
	SchemaTokenRevoke   = "platform.token.revoke"
	tokenPrefix         = "pat_"
)

// Token is a personal API token as the directory keeps it. The secret is
// derived, when the issuing decision applies, from the host's signing key and
// the decision's change id (HMAC), so replay on the same host yields the
// same secret and the journal holds neither it nor its hash; a host with a
// new key retires every token.
type Token struct {
	ID     string    `json:"id"`
	Member string    `json:"member"`
	Label  string    `json:"label"`
	Change string    `json:"change"` // the issuing decision
	Scopes []string  `json:"scopes,omitempty"`
	Until  string    `json:"until,omitempty"`
	Issued time.Time `json:"issued"`
}

// TokenView is a token as its owner sees it.
type TokenView struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Scopes   []string  `json:"scopes"`
	Until    string    `json:"until,omitempty"`
	Issued   time.Time `json:"issued"`
	LastUsed time.Time `json:"lastUsed,omitzero"`
	Expired  bool      `json:"expired"`
}

// Session is the host's memory of a credential: when first and last seen,
// from what, and whether it is the one asking.
type Session struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"` // sign-in, token
	Token   string    `json:"token,omitempty"`
	Agent   string    `json:"agent,omitempty"` // user agent summary
	First   time.Time `json:"first"`
	Last    time.Time `json:"last"`
	Current bool      `json:"current"`
}

type sessionTable struct {
	mu      sync.Mutex
	seen    map[string]map[string]*Session // member → session id → session
	revoked map[string]bool                // session ids a member ended
}

func lifecycleActions() []platform.Action {
	admin := []string{Admin}
	reason := platform.Field{Name: "reason", Type: "string"}
	return []platform.Action{
		{Schema: SchemaInvite, Target: MemberType, Capability: "members", Title: "Invite member",
			Description: "Add a member who has not signed in yet: they stand invited until they first do, and may be granted roles meanwhile.",
			Payload: []platform.Field{{Name: "subject", Type: "string", Required: true, Description: "user:<email> or client:<id>"},
				{Name: "agent", Type: "boolean", Description: "An AI agent"}}, Roles: admin},
		{Schema: SchemaMemberSuspend, Target: MemberType, Capability: "members", Title: "Suspend member",
			Description: "The member keeps their roles on record but holds nothing and cannot act until resumed; their sessions end.", Payload: []platform.Field{reason}, Roles: admin},
		{Schema: SchemaMemberResume, Target: MemberType, Capability: "members", Title: "Resume member",
			Description: "A suspended member holds their roles again.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaOffboard, Target: MemberType, Capability: "members", Title: "Offboard member",
			Description: "The member leaves: their subjects and tokens go, their grants end, their record and history stay. Not reversible; add them again if they return.",
			Payload:     []platform.Field{reason}, Roles: admin},
		{Schema: SchemaTokenIssue, Target: TokenType, Capability: "account", Title: "Issue personal token",
			Description: "A credential for scripts and integrations that acts as you, within the permissions you name, until a day. The secret is shown once.",
			Payload: []platform.Field{{Name: "label", Type: "string", Required: true}, {Name: "scopes", Type: "json", Description: "Permission patterns, such as mes.order.* ; empty: everything you may do"},
				{Name: "until", Type: "string", Description: "Until this day, exclusive (YYYY-MM-DD)"}}, Roles: []string{platform.AnyMember}},
		{Schema: SchemaTokenRevoke, Target: TokenType, Capability: "account", Title: "Revoke personal token",
			Description: "The token stops working at once. Administrators revoke anyone's.", Payload: []platform.Field{}, Roles: []string{platform.AnyMember}},
	}
}

// decideLifecycle decides a member's standing. The console's lock is held.
func (d *Console) decideLifecycle(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	var p struct {
		Subject, Reason string
		Agent           bool
	}
	if json.Unmarshal(s.GetPayload(), &p) != nil {
		return nil, invalid
	}
	id := s.GetTarget().GetId()
	m := d.members[id]
	if s.GetSchema().GetName() == SchemaInvite {
		if !strings.HasPrefix(p.Subject, "user:") && !strings.HasPrefix(p.Subject, "client:") {
			return nil, invalid
		}
		if m != nil || d.subjects[p.Subject] != "" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		return func(*pb.ChangeRecord) {
			d.members[id] = &platform.Member{ID: id, Tenant: d.tenant, Roles: map[string]string{}, Agent: p.Agent, Status: platform.MemberInvited}
			d.subjects[p.Subject] = id
		}, nil
	}
	if m == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if m.Status == platform.MemberLeft || id == c.ID && !c.Replaying {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} may not change their own standing, and one who left has none", c.ID)
	}
	switch s.GetSchema().GetName() {
	case SchemaMemberSuspend:
		return func(*pb.ChangeRecord) { m.Status = platform.MemberSuspended; d.endSessions(id, "") }, nil
	case SchemaMemberResume:
		if m.Status != platform.MemberSuspended {
			return nil, invalid
		}
		return func(*pb.ChangeRecord) { m.Status = "" }, nil
	case SchemaOffboard:
		return func(*pb.ChangeRecord) {
			m.Status = platform.MemberLeft
			m.Grants, m.Roles = nil, map[string]string{}
			maps.DeleteFunc(d.subjects, func(_, member string) bool { return member == id })
			maps.DeleteFunc(d.tokens, func(_ string, t *Token) bool { return t.Member == id })
			d.endSessions(id, "")
		}, nil
	}
	return nil, invalid
}

// decideToken issues or revokes a personal token (ADR-0079 §5). The directory
// keeps its issuing change, never the secret; Minted derives it for collection.
func (d *Console) decideToken(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	id := s.GetTarget().GetId()
	if id == "" {
		return nil, invalid
	}
	if s.GetSchema().GetName() == SchemaTokenRevoke {
		t := d.tokens[id]
		if t == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if t.Member != c.ID && !c.Holds(PlatformApp, Admin) && !c.Replaying {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return func(*pb.ChangeRecord) { delete(d.tokens, id); d.endSessions(t.Member, id) }, nil
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
	if c.Scopes != nil && !c.Replaying { // a token does not mint tokens
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return func(r *pb.ChangeRecord) {
		d.tokens[id] = &Token{ID: id, Member: c.ID, Label: p.Label, Change: r.GetChangeId(), Scopes: p.Scopes, Until: p.Until, Issued: r.GetRecordedTime().AsTime()}
	}, nil
}

var (
	tokenKeyMu sync.RWMutex
	tokenKey   = []byte("development-only") // SignWith replaces it with the host's key
)

// UseTokenKey makes personal tokens derive from key: the lightweight host's
// signing key, or whatever a deployment keeps secret.
func UseTokenKey(key []byte) {
	tokenKeyMu.Lock()
	defer tokenKeyMu.Unlock()
	tokenKey = append([]byte{}, key...)
}

func tokenSecret(tenant, id, change string) string {
	tokenKeyMu.RLock()
	defer tokenKeyMu.RUnlock()
	mac := hmac.New(sha256.New, tokenKey)
	mac.Write([]byte(tenant + "/" + id + "/" + change))
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:24])
}

func tokenHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// mintWindow is how long after issue a token's secret may be collected.
const mintWindow = 10 * time.Minute

// A recovered token remains usable, but its one-time delivery window belongs
// to the issuing process. Restarting must not reopen a collected secret.
func (d *Console) closeTokenSecretWindows() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for id := range d.tokens {
		d.minted[id] = true
	}
}

// Minted hands out a token's secret once, to the member who issued it,
// within mintWindow of the issue.
func (d *Console) Minted(id, member string, now time.Time) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t := d.tokens[id]
	if t == nil || t.Member != member || d.minted[id] || now.Sub(t.Issued) > mintWindow {
		return "", false
	}
	d.minted[id] = true
	return tokenSecret(d.tenant, id, t.Change), true
}

// MemberByToken is the member a personal token signs in as, bounded to its
// scopes; false for an unknown, expired or revoked token, or a member who
// cannot act.
func (d *Console) MemberByToken(secret string, now time.Time) (platform.Member, bool) {
	if !strings.HasPrefix(secret, tokenPrefix) {
		return platform.Member{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, t := range d.tokens {
		if !hmac.Equal([]byte(tokenSecret(d.tenant, t.ID, t.Change)), []byte(secret)) {
			continue
		}
		m := d.members[t.Member]
		if m == nil || t.Until != "" && now.UTC().Format(time.DateOnly) >= t.Until {
			return platform.Member{}, false
		}
		if now.Sub(d.lastSeen["token:"+t.ID]) >= time.Minute {
			d.lastSeen["token:"+t.ID] = now // host memory, like a member's last sign-in
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

// Tokens are a member's tokens, as the owner sees them.
func (d *Console) Tokens(member string, now time.Time) []TokenView {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []TokenView{}
	for _, id := range slices.Sorted(maps.Keys(d.tokens)) {
		t := d.tokens[id]
		if t.Member != member {
			continue
		}
		scopes := t.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		out = append(out, TokenView{ID: t.ID, Label: t.Label, Scopes: scopes, Until: t.Until, Issued: t.Issued, LastUsed: d.lastSeen["token:"+t.ID],
			Expired: t.Until != "" && now.UTC().Format(time.DateOnly) >= t.Until})
	}
	return out
}

// Sessions ----------------------------------------------------------------

func sessionID(credential string) string { return tokenHash(credential)[:16] }

// noticed records that a credential acted as member now; false when the
// member ended that session, which the host then refuses.
func (d *Console) noticed(member, credential, agent string, kind string, now time.Time) bool {
	id := sessionID(credential)
	token := ""
	if kind == "token" {
		d.mu.Lock()
		for key, t := range d.tokens {
			if t.Member == member && hmac.Equal([]byte(tokenSecret(d.tenant, t.ID, t.Change)), []byte(credential)) {
				token = key
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
		d.sessions.seen[member] = map[string]*Session{}
	}
	s := d.sessions.seen[member][id]
	if s == nil {
		s = &Session{ID: id, Kind: kind, Token: token, First: now, Agent: summarize(agent)}
		d.sessions.seen[member][id] = s
	}
	s.Last = now
	return true
}

// endSessions forgets a member's sessions and refuses their credentials: all
// of them, or those of one token.
func (d *Console) endSessions(member, token string) {
	d.sessions.mu.Lock()
	defer d.sessions.mu.Unlock()
	for id, s := range d.sessions.seen[member] {
		if token == "" || s.Token == token {
			d.sessions.revoked[id] = true
			delete(d.sessions.seen[member], id)
		}
	}
}

// EndOtherSessions keeps only the session asking.
func (d *Console) EndOtherSessions(member, credential string) int {
	keep := sessionID(credential)
	d.sessions.mu.Lock()
	defer d.sessions.mu.Unlock()
	n := 0
	for id := range d.sessions.seen[member] {
		if id != keep {
			d.sessions.revoked[id] = true
			delete(d.sessions.seen[member], id)
			n++
		}
	}
	return n
}

// Sessions are a member's, newest last seen first, the current one marked.
func (d *Console) Sessions(member, credential string) []Session {
	cur := sessionID(credential)
	d.sessions.mu.Lock()
	defer d.sessions.mu.Unlock()
	out := []Session{}
	for _, s := range d.sessions.seen[member] {
		v := *s
		v.Current = v.ID == cur
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b Session) int { return b.Last.Compare(a.Last) })
	return out
}

// summarize keeps of a user agent what a person recognises.
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
