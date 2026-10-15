package buildapi

import "platformserver/platform"

const ObservationType = "build.observation"
const SchemaObservation = ObservationType + ".publish"

// Observation is a portable read/decision declaration over an external store.
// It never holds samples, summaries or credentials.
type Observation struct {
	platform.Record
	Name                       string                        `json:"name" field:"required,search" source:"record"`
	Title                      string                        `json:"title" field:"required,search" source:"record"`
	Description                string                        `json:"description,omitempty" type:"longtext"`
	Connection                 string                        `json:"connection" field:"required" ref:"build.connection" source:"record"`
	Object                     string                        `json:"object" field:"required"`
	DeviceField                string                        `json:"deviceField" field:"required" title:"Device identity field"`
	Signal                     string                        `json:"signal" field:"required"`
	Unit                       string                        `json:"unit" field:"required"`
	Path                       string                        `json:"path" field:"required" help:"Relative adapter path on the logical connection"`
	WindowSeconds              int                           `json:"windowSeconds" field:"required" title:"Decision window in seconds"`
	ExpectedRetentionSeconds   int                           `json:"expectedRetentionSeconds" field:"required" title:"Expected external retention in seconds"`
	ExpectedGranularitySeconds int                           `json:"expectedGranularitySeconds" field:"required" title:"Expected external granularity in seconds"`
	Roles                      []string                      `json:"roles" field:"required"`
	Scheduled                  bool                          `json:"scheduled,omitempty" title:"Evaluate completed windows"`
	RequireComplete            bool                          `json:"requireComplete,omitempty" title:"Require complete, good-quality windows"`
	Decision                   *platform.ObservationDecision `json:"decision,omitempty" type:"json"`
	State                      string                        `json:"state" field:"readonly" choices:"draft,published,retired"`
	Publisher                  string                        `json:"publisher,omitempty" field:"readonly" source:"record"`
	PublisherScopes            string                        `json:"publisherScopes" field:"readonly" type:"json" title:"Publisher scopes"`
	DefinitionVersion          int                           `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published                  string                        `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed" source:"record"`
	Versions                   []string                      `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}
