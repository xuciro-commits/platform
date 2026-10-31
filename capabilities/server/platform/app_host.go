package platform

import (
	"time"
)

type SupportGrant struct {
	ID      string    `json:"id"`
	Tenant  string    `json:"tenant"`
	Member  string    `json:"member"`
	Reason  string    `json:"reason"`
	Opened  string    `json:"opened"`
	Expires time.Time `json:"expires"`
	Uses    int       `json:"uses"`
}

type MigrationType struct {
	Type    string      `json:"type"`
	Rows    int         `json:"rows"`
	Written int         `json:"written"`
	Refused []ImportRow `json:"refused,omitempty"`
}

type MigrationResult struct {
	From  string          `json:"from"`
	To    string          `json:"to"`
	Key   string          `json:"key"`
	Types []MigrationType `json:"types"`
}

type MigrationManifest struct {
	At     time.Time       `json:"at"`
	From   string          `json:"from"`
	To     string          `json:"to"`
	Key    string          `json:"key"`
	Member string          `json:"member"`
	Types  []MigrationType `json:"types"`
}

type ImportRow struct {
	Row     int        `json:"row"`
	ID      string     `json:"id"`
	Action  string     `json:"action"`
	Outcome string     `json:"outcome"`
	Error   *ErrorBody `json:"error,omitempty"`
}

type ErrorBody struct {
	Message string       `json:"message,omitempty"`
	Issues  []FieldIssue `json:"issues,omitempty"`
	Code    string       `json:"code"`
	Status  int          `json:"status,omitempty"`
	Detail  string       `json:"detail,omitempty"`
}

type HostControlImage struct {
	Lifecycle        string                        `json:"lifecycle"`
	Support          []SupportGrant                `json:"support"`
	Migrations       []MigrationManifest           `json:"migrations"`
	Requests         map[string]HostControlReceipt `json:"requests"`
	RuntimeSelection map[string]string             `json:"runtimeSelection,omitempty"`
	DisabledOwners   []string                      `json:"disabledOwners,omitempty"`
}

type HostControlReceipt struct {
	RequestHash string       `json:"requestHash"`
	Answer      SupportGrant `json:"answer,omitempty"`
}

type TenantSpec struct {
	ID       string            `json:"id"`
	Name     string            `json:"name,omitempty"`
	Template string            `json:"template,omitempty"`
	Seats    []Seat            `json:"seats"`
	Settings map[string]string `json:"settings,omitempty"`
	Created  time.Time         `json:"created,omitempty"`
	By       string            `json:"by,omitempty"`
}

type TenantTemplate struct {
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Settings    map[string]string `json:"settings,omitempty"`
	AdminRoles  map[string]string `json:"adminRoles,omitempty"`
}

type CreateTenantRequest struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Template string            `json:"template,omitempty"`
	Admin    string            `json:"admin"`
	Settings map[string]string `json:"settings,omitempty"`
}
