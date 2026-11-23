package ontology

const (
	AppID                 = "build"
	ObjectType            = AppID + ".object"
	ActionTypeType        = AppID + ".actiontype"
	PropertyTypeType      = AppID + ".propertytype"
	ValueTypeType         = AppID + ".valuetype"
	ObjectTypeGroupType   = AppID + ".objecttypegroup"
	LinkTypeType          = AppID + ".linktype"
	InterfaceType         = AppID + ".interface"
	ContractType          = AppID + ".contract"
	BuilderRole           = "builder"
	UserRole              = "user"
	SchemaPublish         = ObjectType + ".publish"
	SchemaActionType      = ActionTypeType + ".publish"
	SchemaPropertyType    = PropertyTypeType + ".publish"
	SchemaValueType       = ValueTypeType + ".publish"
	SchemaObjectTypeGroup = ObjectTypeGroupType + ".publish"
	SchemaLinkType        = LinkTypeType + ".publish"
	SchemaInterface       = InterfaceType + ".publish"
	SchemaContract        = ContractType + ".publish"
)
