package protocol

import "time"

type MeTenant struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type IdentityEntry struct {
	Subject     string             `json:"subject"`
	Workspaces  []MeTenant         `json:"workspaces"`
	Invitations []PendingWorkspace `json:"invitations"`
	CanCreate   bool               `json:"canCreate"`
}

type PendingWorkspace struct {
	MeTenant
	Expires time.Time `json:"expires"`
}

type WorkspaceCreate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Language string `json:"language,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}
