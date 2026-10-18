package enterprise

import "platformserver/internal/host/enterpriseapi"

type Contract = enterpriseapi.Contract
type Date = enterpriseapi.Date
type Element = enterpriseapi.Element
type Kind = enterpriseapi.Kind
type Model = enterpriseapi.Model
type Pair = enterpriseapi.Pair
type Params = enterpriseapi.Params
type Pattern = enterpriseapi.Pattern
type Pin = enterpriseapi.Pin
type Preview = enterpriseapi.Preview
type ProfileEntry = enterpriseapi.ProfileEntry
type Relationship = enterpriseapi.Relationship
type SeedParams = enterpriseapi.SeedParams
type Slice = enterpriseapi.Slice
type SyncResult = enterpriseapi.SyncResult
type View = enterpriseapi.View
type Viewpoint = enterpriseapi.Viewpoint
type Period = enterpriseapi.Period
type TenantLink = enterpriseapi.TenantLink

const (
	Capability   = enterpriseapi.Capability
	Exhibits     = enterpriseapi.Exhibits
	FillsPost    = enterpriseapi.FillsPost
	Goal         = enterpriseapi.Goal
	Location     = enterpriseapi.Location
	Membership   = enterpriseapi.Membership
	Organization = enterpriseapi.Organization
	Owns         = enterpriseapi.Owns
	Performs     = enterpriseapi.Performs
	Person       = enterpriseapi.Person
	Placement    = enterpriseapi.Placement
	Post         = enterpriseapi.Post
	Project      = enterpriseapi.Project
	Resource     = enterpriseapi.Resource
	System       = enterpriseapi.System
	Typed        = enterpriseapi.Typed
)
