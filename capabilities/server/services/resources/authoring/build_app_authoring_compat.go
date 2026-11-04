package build

import appsauthoring "platformserver/services/apps/authoring"

type Page = appsauthoring.Page
type Section = appsauthoring.Section
type InlineEdit = appsauthoring.InlineEdit
type Application = appsauthoring.Application
type Group = appsauthoring.Group

const (
	PageType       = appsauthoring.PageType
	SchemaRelease  = appsauthoring.SchemaRelease
	AppType        = appsauthoring.AppType
	SchemaHandOver = appsauthoring.SchemaHandOver
)
