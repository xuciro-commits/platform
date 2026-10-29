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
	"os"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Member lifecycle, personal tokens and sessions (–5). A member's
// standing is a decision of the directory: invited when added before they
// sign in, suspended (nothing is held until resumed), left (offboarded: the
// subjects go, the grants go, the record stays for history). A personal token
// is a credential the host issues for scripts and integrations, derived from a private key,
// bounded in time and scope; a session is the host's memory of a credential
// it saw, which a member may end.
const (
	SchemaInvite        = "platform.member.invite"
	SchemaJoin          = "platform.member.join"
	SchemaMemberSuspend = "platform.member.suspend"
	SchemaMemberResume  = "platform.member.resume"
	SchemaOffboard      = "platform.member.offboard"
	TokenType           = "platform.token"
	SchemaTokenIssue    = "platform.token.issue"
	SchemaTokenRevoke   = "platform.token.revoke"
	TokenPrefix         = "pat_"
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

// endedSessionsShown is how long an ended session stays in the list.
const endedSessionsShown = 24 * time.Hour

type sessionTable struct {
	mu      sync.Mutex
	seen    map[string]map[string]*platform.Session // member → session id → session
	revoked map[string]bool                         // session ids a member ended
}

// end marks a session ended now; the host refuses its credential from here.
func (st *sessionTable) end(member, id string, now time.Time) {
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
			Description: "Add a member who has not signed in yet: they stand invited until they first do, and may be granted roles meanwhile.",
			Payload: []platform.Field{{Name: "subject", Type: "string", Required: true, Description: "user:<email> or client:<id>"},
				{Name: "agent", Type: "boolean", Description: "An AI agent"}}, Roles: admin},
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
			Payload: []platform.Field{{Name: "label", Type: "string", Required: true}, {Name: "scopes", Type: "json", Description: "Permission patterns, such as core.material.* ; empty: everything you may do"},
				{Name: "until", Type: "string", Description: "Until this day, exclusive (YYYY-MM-DD)"}}, Roles: []string{platform.AnyMember}},
		{Schema: SchemaTokenRevoke, Target: TokenType, Capability: "account", Title: "Revoke personal token",
			Description: "The token stops working at once. Administrators revoke anyone's.", Payload: []platform.Field{}, Roles: []string{platform.AnyMember}},
	}
}

// decideLifecycle decides a member's standing. The console's lock is held.
func (d *Console) decideLifecycle(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
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
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
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
		return func(r *pb.ChangeRecord) {
			d.members[id] = &platform.Member{ID: id, Tenant: d.tenant, Roles: map[string]string{}, Agent: p.Agent, Status: platform.MemberInvited}
			d.subjects[p.Subject] = id
			d.then = func() { // the invitation, mailed where the tenant mails; nothing when it does not
				org := cmp.Or(d.tenantDefault(SettingName), d.tenant)
				c.Notify(platform.Notification{Title: "You are invited to " + org, Key: "invite",
					Body: "Sign in with " + strings.TrimPrefix(p.Subject, "user:") + " to take your seat at " + org + "."},
					r.GetRecordedTime().AsTime(), platform.Recipient{Member: id})
			}
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
		return func(r *pb.ChangeRecord) {
			m.Status = platform.MemberSuspended
			d.endSessions(id, "", r.GetRecordedTime().AsTime())
		}, nil
	case SchemaMemberResume:
		if m.Status != platform.MemberSuspended {
			return nil, invalid
		}
		return func(*pb.ChangeRecord) { m.Status = "" }, nil
	case SchemaOffboard:
		successor := d.members[p.Successor]
		if p.Successor != "" && (successor == nil || p.Successor == id || !d.currentMember(successor).Active()) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} cannot take over: not an active member", p.Successor)
		}
		return func(r *pb.ChangeRecord) {
			m.Status = platform.MemberLeft
			m.Grants, m.Roles = nil, map[string]string{}
			if !c.Staging() { // legacy input replay retains its original subject-removal semantics
				maps.DeleteFunc(d.subjects, func(_, member string) bool { return member == id })
			} // accepted results retain a denial binding; only explicit add/invite may replace it
			maps.DeleteFunc(d.tokens, func(_ string, t *Token) bool { return t.Member == id })
			d.endSessions(id, "", r.GetRecordedTime().AsTime())
			// What they held through others' delegation ends; what others held through theirs passes on.
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
			}
			d.then = func() { d.handOver(c, id, p.Successor, p.Reason, r.GetRecordedTime().AsTime()) }
		}, nil
	}
	return nil, invalid
}

// decideToken issues or revokes a personal token. The directory
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
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:24])
}

func tokenHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// mintWindow is how long after issue a token's secret may be collected.
const mintWindow = 10 * time.Minute

// A recovered token remains usable, but its one-time delivery window belongs
// to the issuing process. Restarting must not reopen a collected secret.
func (d *Console) CloseTokenSecretWindows() {
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
	if !strings.HasPrefix(secret, TokenPrefix) {
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
func (d *Console) Tokens(member string, now time.Time) []platform.TokenView {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []platform.TokenView{}
	for _, id := range slices.Sorted(maps.Keys(d.tokens)) {
		t := d.tokens[id]
		if t.Member != member {
			continue
		}
		scopes := t.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		out = append(out, platform.TokenView{ID: t.ID, Label: t.Label, Scopes: scopes, Until: t.Until, Issued: t.Issued, LastUsed: d.lastSeen["token:"+t.ID],
			Expired: t.Until != "" && now.UTC().Format(time.DateOnly) >= t.Until})
	}
	return out
}

func sessionID(credential string) string { return tokenHash(credential)[:16] }

// noticed records that a credential acted as member now; false when the
// member ended that session, which the host then refuses.
func (d *Console) NoticeSession(member, credential, agent string, kind string, now time.Time) bool {
	id := sessionID(credential)
	hours := 0
	if d.host != nil {
		hours, _ = strconv.Atoi(d.tenantDefault(SettingSessionHours))
	}
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
		d.sessions.seen[member] = map[string]*platform.Session{}
	}
	s := d.sessions.seen[member][id]
	if s != nil && kind == "sign-in" && hours > 0 && now.Sub(s.First) >= time.Duration(hours)*time.Hour {
		d.sessions.end(member, id, now) // only a newly authenticated credential may start another session
		return false
	}
	if s == nil {
		s = &platform.Session{ID: id, Kind: kind, Token: token, First: now, Agent: summarize(agent)}
		d.sessions.seen[member][id] = s
	}
	s.Last = now
	return true
}

// endSessions forgets a member's sessions and refuses their credentials: all
// of them, or those of one token.
func (d *Console) endSessions(member, token string, now time.Time) {
	d.sessions.mu.Lock()
	defer d.sessions.mu.Unlock()
	for id, s := range d.sessions.seen[member] {
		if (token == "" || s.Token == token) && s.Ended.IsZero() {
			d.sessions.end(member, id, now)
		}
	}
}

// EndOtherSessions keeps only the session asking.
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

// Sessions are a member's, newest last seen first, the current one marked;
// sessions ended within the last day stay listed, marked ended, then go.
func (d *Console) Sessions(member, credential string, now time.Time) []platform.Session {
	cur := sessionID(credential)
	d.sessions.mu.Lock()
	defer d.sessions.mu.Unlock()
	out := []platform.Session{}
	for id, s := range d.sessions.seen[member] {
		if !s.Ended.IsZero() && now.Sub(s.Ended) > endedSessionsShown {
			delete(d.sessions.seen[member], id)
			continue
		}
		v := *s
		v.Current = v.ID == cur
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b platform.Session) int {
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

// joins reports whether subject may take a seat by its mail domain alone: a
// person of one of the tenant's sign-in domains.
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

// SecondFactorRequired reports whether every sign-in to this tenant must be
// attested with a second factor by the identity provider.
func (d *Console) SecondFactorRequired() bool {
	return d.host != nil && d.tenantDefault(SettingMFA) == "true"
}

// JoinID is the member id a self-joining subject takes: the local part of the
// address, made a slug, with a number when taken.
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

// handOver, after a member left, tells the successor what they take over.
// The enterprise model ends the member's tenure itself (it subscribes to the
// offboarding); the apps' open items move by their own reassignment actions,
// which the notice points at.
func (d *Console) handOver(c platform.Caller, left, successor, reason string, now time.Time) {
	if successor == "" {
		return
	}
	c.Notify(platform.Notification{Title: "You take over from " + left, Key: "handover:" + left, Ref: MemberType + "/" + left,
		Body: strings.TrimSpace("Delegations " + left + " gave now stand in your name; reassign their open work and approvals to yourself from each app. " + reason)},
		now, platform.Recipient{Member: successor})
}

// joinable excludes existing identities, including denial bindings to members
// who left. It is checked again under the tenant lock before recording a join.
func (d *Console) JoinableSubject(subject string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.subjects[subject] == ""
}

// specs are the host's tenant specs, from the -tenants file or the binary's development default.
type Specs struct {
	mu   sync.Mutex
	file string
	list []platform.TenantSpec
}

func (s *Specs) Load(file string, development platform.TenantSpec) error {
	s.file = file
	if file == "" {
		s.list = []platform.TenantSpec{development}
		return nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var list []platform.TenantSpec
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

func (s *Specs) Get(id string) (platform.TenantSpec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.list {
		if t.ID == id {
			return t, true
		}
	}
	return platform.TenantSpec{}, false
}

func (s *Specs) All() []platform.TenantSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.list)
}

// add appends a spec and writes the file back, so the tenant exists on the next start too.
func (s *Specs) Add(spec platform.TenantSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.ContainsFunc(s.list, func(t platform.TenantSpec) bool { return t.ID == spec.ID }) {
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

// tenantRegistry keeps the host, work runner, telemetry and snapshotter on the
// same tenant generation. Rebuilding never reuses a partially restored Tenant.
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

// add hosts a new tenant; ids are unique.
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
