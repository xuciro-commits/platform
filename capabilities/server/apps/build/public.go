// Package build owns construction authoring and publication.
//
// This file is the only public surface for consumers outside the platform
// module (the industry solutions). Inside the module everything imports
// internal/construction directly, so unread names are not re-exported.
package build

import "platformserver/internal/construction"

const AppType = construction.AppType
const SchemaHandOver = construction.SchemaHandOver
const PageType = construction.PageType
const SchemaRelease = construction.SchemaRelease
const ID = construction.ID
const ObjectType = construction.ObjectType
const Builder = construction.Builder
const User = construction.User
const SchemaPublish = construction.SchemaPublish
const SchemaFunctionCall = construction.SchemaFunctionCall
const TestPlanType = construction.TestPlanType

type TestPlan = construction.TestPlan
type TestStep = construction.TestStep
