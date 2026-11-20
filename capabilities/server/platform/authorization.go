package platform

type AuthorizationEngine interface {
	Decide(AuthorizationInput, ...AuthorizationPolicy) AuthzDecision
	Widest([]string) string
}

type AuthorizationSubject struct {
	ID, App                      string
	Roles                        []string
	Agent, Automation, Replaying bool
}

type AuthorizationInput struct {
	Kind                 string
	Subject              AuthorizationSubject
	Permission, Resource string
	Allowed              []string
	Attributes           map[string]string
	Context              *AuthorizationContext
}

type AuthorizationContext struct {
	Tenant, Principal              AuthorizationGate
	Verb                           *AuthorizationGate
	Row, Fields, Derived, Markings AuthorizationGate
	Withheld                       []string
	Definition                     string
}

type AuthorizationGate struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

type Layer struct {
	Name    string `json:"name"`
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
	Role    string `json:"role,omitempty"`
	Policy  string `json:"policy,omitempty"`
}

type Verdict struct {
	Allow      bool     `json:"allow"`
	Rule       string   `json:"rule"`
	Reason     string   `json:"reason"`
	Role       string   `json:"role,omitempty"`
	Policy     string   `json:"policy,omitempty"`
	Layers     []Layer  `json:"layers"`
	Withheld   []string `json:"withheld,omitempty"`
	Definition string   `json:"definition,omitempty"`
}

type AuthorizationPolicy struct {
	ID, Effect, Permission string
	When                   func(AuthorizationInput) bool
}
