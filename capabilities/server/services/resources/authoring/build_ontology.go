package build

import (
	ontology "platformserver/services/ontology"
)

type (
	Object              = ontology.Object
	Field               = ontology.Field
	State               = ontology.State
	Action              = ontology.Action
	JournalPost         = ontology.JournalPost
	JournalPostLine     = ontology.JournalPostLine
	Create              = ontology.Create
	ActionApproval      = ontology.ActionApproval
	ApproverLevel       = ontology.ApproverLevel
	Input               = ontology.Input
	Set                 = ontology.Set
	Condition           = ontology.Condition
	Access              = ontology.Access
	ObjectScope         = ontology.ObjectScope
	Numbering           = ontology.Numbering
	Post                = ontology.Post
	PropertyType        = ontology.PropertyType
	ValueType           = ontology.ValueType
	ObjectTypeGroup     = ontology.ObjectTypeGroup
	ActionType          = ontology.ActionType
	ActionTypeVersion   = ontology.ActionTypeVersion
	LinkType            = ontology.LinkType
	InterfaceRecord     = ontology.InterfaceRecord
	InterfaceDefinition = ontology.InterfaceDefinition
)

const (
	BaseField             = ontology.BaseField
	ObjectType            = ontology.ObjectType
	ActionTypeType        = ontology.ActionTypeType
	PropertyTypeType      = ontology.PropertyTypeType
	ValueTypeType         = ontology.ValueTypeType
	ObjectTypeGroupType   = ontology.ObjectTypeGroupType
	SchemaPublish         = ontology.SchemaPublish
	SchemaExtractActions  = ontology.SchemaExtractActions
	SchemaActionType      = ontology.SchemaActionType
	SchemaPropertyType    = ontology.SchemaPropertyType
	SchemaValueType       = ontology.SchemaValueType
	SchemaObjectTypeGroup = ontology.SchemaObjectTypeGroup
	LinkTypeType          = ontology.LinkTypeType
	InterfaceType         = ontology.InterfaceType
	SchemaLinkType        = ontology.SchemaLinkType
	SchemaInterface       = ontology.SchemaInterface
)
