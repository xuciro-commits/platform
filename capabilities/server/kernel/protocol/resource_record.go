package protocol

import "platformserver/platform"

type Resource struct {
	platform.Record
	Ref       platform.AssetRef `json:"ref" field:"readonly" type:"json"`
	Type      string            `json:"type,omitempty" field:"readonly"`
	Draft     string            `json:"draft,omitempty" field:"readonly"`
	Variant   string            `json:"variant,omitempty" field:"readonly"`
	Title     string            `json:"title" field:"readonly,search"`
	Project   string            `json:"project" field:"readonly"`
	Folder    string            `json:"folder,omitempty" field:"readonly"`
	Version   uint32            `json:"version" field:"readonly"`
	State     string            `json:"state" field:"readonly"`
	ManagedBy string            `json:"managedBy,omitempty" field:"readonly"`
}
