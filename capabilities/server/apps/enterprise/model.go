package enterprise

import (
	enterprisemodel "platformserver/internal/enterprise"
	"platformserver/platform"
)

// These names preserve the application package's public source API. Internal
// consumers use the enterprise model owner directly rather than these aliases.
type Contract = enterprisemodel.Contract
type Date = platform.Date
type Element = enterprisemodel.Element
type Kind = enterprisemodel.Kind
type Model = enterprisemodel.Model
type Pair = enterprisemodel.Pair
type Pin = enterprisemodel.Pin
type ProfileEntry = enterprisemodel.ProfileEntry
type Relationship = enterprisemodel.Relationship
type Slice = enterprisemodel.Slice
type SyncResult = enterprisemodel.SyncResult
type View = enterprisemodel.View
type Viewpoint = enterprisemodel.Viewpoint
type Period = platform.Period
type TenantLink = enterprisemodel.TenantLink

const (
	Capability   = enterprisemodel.Capability
	Exhibits     = enterprisemodel.Exhibits
	FillsPost    = enterprisemodel.FillsPost
	Goal         = enterprisemodel.Goal
	Location     = enterprisemodel.Location
	Membership   = enterprisemodel.Membership
	Organization = enterprisemodel.Organization
	Owns         = enterprisemodel.Owns
	Performs     = enterprisemodel.Performs
	Person       = enterprisemodel.Person
	Placement    = enterprisemodel.Placement
	Post         = enterprisemodel.Post
	Project      = enterprisemodel.Project
	Resource     = enterprisemodel.Resource
	System       = enterprisemodel.System
	Typed        = enterprisemodel.Typed
)
