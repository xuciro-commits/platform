package ontology

import (
	"slices"

	"platformserver/platform"
)

type LinkType struct {
	platform.Record
	Storage      string   `json:"storage,omitempty" choices:"reference,edge"`
	Roles        []string `json:"roles,omitempty" field:"aside"`
	DeletePolicy string   `json:"deletePolicy,omitempty" choices:"owner,restrict-active" title:"Relationship archive policy"`
	Cardinality  string   `json:"cardinality,omitempty" choices:"one-to-many,one-to-one,many-to-many" title:"Relationship cardinality"`
	Name         string   `json:"name" field:"required,search"`
	Title        string   `json:"title" field:"required,search"`
	Description  string   `json:"description" field:"required" type:"longtext"`
	Parent       string   `json:"parent" field:"required"`
	Child        string   `json:"child" field:"required"`
	Via          string   `json:"via,omitempty"`
	Forward      string   `json:"forward" field:"required"`
	Reverse      string   `json:"reverse" field:"required"`
	Required     bool     `json:"required" title:"Parent relationship required"`
	State        string   `json:"state" field:"readonly" choices:"draft,published"`
	Version      int      `json:"version,omitempty" field:"readonly"`
	Published    string   `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions     []string `json:"versions,omitempty" field:"readonly"`
}

func (f LinkType) Definition() platform.LinkType {
	storage := f.Storage
	if storage == "" {
		storage = "reference"
	}
	roles := f.Roles
	if storage == "edge" && len(roles) == 0 {
		roles = []string{BuilderRole, UserRole}
	}
	policy := f.DeletePolicy
	if policy == "" {
		policy = "owner"
	}
	cardinality := f.Cardinality
	if cardinality == "" {
		cardinality = "one-to-many"
	}
	return platform.LinkType{Name: f.Name, Title: f.Title, Description: f.Description, Parent: platform.AssetRef{App: AppID, Kind: platform.AssetObject, Name: f.Parent}, Child: platform.AssetRef{App: AppID, Kind: platform.AssetObject, Name: f.Child}, Via: f.Via, Forward: f.Forward, Reverse: f.Reverse, Required: f.Required, Storage: storage, Cardinality: cardinality, DeletePolicy: policy, Roles: slices.Clone(roles)}
}
