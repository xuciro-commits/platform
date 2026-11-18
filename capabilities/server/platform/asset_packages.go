package platform

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/capability"
)

type PackageContribution struct {
	View    string `json:"view"`
	Surface string `json:"surface,omitempty"`
	Title   string `json:"title,omitempty"`
	Kind    string `json:"kind,omitempty"`
	App     string `json:"app,omitempty"`
}

type PackageDescriptor struct {
	Blueprint      *PackageBlueprint               `json:"blueprint,omitempty"`
	ID             string                          `json:"id"`
	Version        string                          `json:"version"`
	Title          string                          `json:"title,omitempty"`
	Namespace      string                          `json:"namespace"`
	Requires       []string                        `json:"requires,omitempty"`
	Capabilities   []capability.CapabilityContract `json:"capabilities,omitempty"`
	Compatibility  string                          `json:"compatibility,omitempty"`
	Contributions  []PackageContribution           `json:"contributions,omitempty"`
	ArtifactDigest string                          `json:"artifactDigest,omitempty"`
	Description    string                          `json:"description,omitempty"`
}

type PackageBlueprint struct {
	Roots  []AssetRef     `json:"roots"`
	Assets []ReleaseAsset `json:"assets"`
	Guide  string         `json:"guide,omitempty"`
	Setup  *PackageSetup  `json:"setup,omitempty"`
}

type PackageSetup struct {
	Roles []PackageSetupRole `json:"roles"`
	Steps []PackageSetupStep `json:"steps"`
}

type PackageSetupRole struct {
	App  string `json:"app"`
	Role string `json:"role"`
}

type PackageSetupStep struct {
	Name    string        `json:"name"`
	App     string        `json:"app"`
	Schema  string        `json:"schema"`
	Target  *pb.EntityRef `json:"target"`
	Payload string        `json:"payload"`
}

type PackageImpactReference struct {
	Resource AssetRef `json:"resource"`
	Consumer AssetRef `json:"consumer"`
}

type PackageImpactExtension struct {
	Type          string       `json:"type"`
	Base          AssetBinding `json:"base"`
	TargetVersion string       `json:"targetVersion"`
	Fields        []string     `json:"fields"`
	Issue         string       `json:"issue,omitempty"`
}

type PackageImpactOutput struct {
	Resource AssetRef `json:"resource"`
	Records  int      `json:"records"`
	Retained bool     `json:"retained"`
}

type PackageImpactConnection struct {
	Resource   AssetRef `json:"resource"`
	Configured bool     `json:"configured"`
	Credential bool     `json:"credential"`
}

type PackageLifecycleImpact struct {
	Dependencies []string                  `json:"dependencies"`
	References   []PackageImpactReference  `json:"references"`
	Extensions   []PackageImpactExtension  `json:"extensions"`
	Runs         []ApplicationRun          `json:"runs"`
	Connections  []PackageImpactConnection `json:"connections"`
	Outputs      []PackageImpactOutput     `json:"outputs"`
}
