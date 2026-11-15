package platform

type ApplicationArtifact struct {
	Samples      []AssetBinding       `json:"samples"`
	Ref          AssetRef             `json:"ref"`
	Version      string               `json:"version"`
	Title        string               `json:"title"`
	Profile      *ApplicationProfile  `json:"profile,omitempty"`
	Model        []AssetBinding       `json:"model"`
	Rules        []AssetBinding       `json:"rules"`
	Pages        []AssetBinding       `json:"pages"`
	Flows        []AssetBinding       `json:"flows"`
	AI           []AssetBinding       `json:"ai"`
	Integrations []AssetBinding       `json:"integrations"`
	Roles        []ApplicationRole    `json:"roles"`
	Depends      []AssetBinding       `json:"depends"`
	Environment  []ApplicationBinding `json:"environment"`
}

type ApplicationRole struct {
	Resource    AssetBinding `json:"resource"`
	Role        string       `json:"role"`
	Scope       string       `json:"scope,omitempty"`
	ReadFields  []string     `json:"readFields,omitempty"`
	WriteFields []string     `json:"writeFields,omitempty"`
}

type ApplicationBinding struct {
	Ref        AssetRef `json:"ref"`
	Kind       string   `json:"kind"`
	Name       string   `json:"name,omitempty"`
	Credential bool     `json:"credential,omitempty"`
}
