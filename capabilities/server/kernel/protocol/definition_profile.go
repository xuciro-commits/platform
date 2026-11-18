package protocol

import (
	"platformserver/platform"
)

type ApplicationArtifact struct {
	Samples      []platform.AssetBinding      `json:"samples"`
	Ref          platform.AssetRef            `json:"ref"`
	Version      string                       `json:"version"`
	Title        string                       `json:"title"`
	Profile      *platform.ApplicationProfile `json:"profile,omitempty"`
	Model        []platform.AssetBinding      `json:"model"`
	Rules        []platform.AssetBinding      `json:"rules"`
	Pages        []platform.AssetBinding      `json:"pages"`
	Flows        []platform.AssetBinding      `json:"flows"`
	AI           []platform.AssetBinding      `json:"ai"`
	Integrations []platform.AssetBinding      `json:"integrations"`
	Roles        []ApplicationRole            `json:"roles"`
	Depends      []platform.AssetBinding      `json:"depends"`
	Environment  []ApplicationBinding         `json:"environment"`
}

type ApplicationRole struct {
	Resource    platform.AssetBinding `json:"resource"`
	Role        string                `json:"role"`
	Scope       string                `json:"scope,omitempty"`
	ReadFields  []string              `json:"readFields,omitempty"`
	WriteFields []string              `json:"writeFields,omitempty"`
}

type ApplicationBinding struct {
	Ref        platform.AssetRef `json:"ref"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name,omitempty"`
	Credential bool              `json:"credential,omitempty"`
}
