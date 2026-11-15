package platform

import "encoding/json"

type PageCollectionInput struct {
	Kind      string             `json:"kind"`
	Object    AssetRef           `json:"object"`
	Predicate RecordSetPredicate `json:"predicate"`
	Sort      []string           `json:"sort"`

	SortLocked bool           `json:"sortLocked,omitempty"`
	Traversal  *LinkTraversal `json:"traversal,omitempty"`
	Bindings   []AssetBinding `json:"bindings,omitempty"`
}

type PageComputeResource struct {
	Operation      AssetBinding       `json:"operation"`
	RecordVariable string             `json:"recordVariable"`
	Inputs         map[string]Binding `json:"inputs"`
	OutputField    string             `json:"outputField,omitempty"`
}

type PageEmbedding struct {
	ReadOnly         bool                 `json:"readOnly,omitempty"`
	Kind             string               `json:"kind"`
	Page             AssetBinding         `json:"page"`
	ContentVersion   string               `json:"contentVersion"`
	InterfaceVersion int                  `json:"interfaceVersion"`
	Inputs           map[string]PageValue `json:"inputs,omitempty"`
	Results          map[string]string    `json:"results,omitempty"`
}

type PageEventBinding struct {
	Control string       `json:"control,omitempty"`
	Source  string       `json:"source"`
	Event   string       `json:"event"`
	Effects []PageEffect `json:"effects"`
}

type PageEffect struct {
	Kind     string            `json:"kind"`
	Target   string            `json:"target,omitempty"`
	Value    json.RawMessage   `json:"value,omitempty"`
	From     string            `json:"from,omitempty"`
	Action   *PageActionEffect `json:"action,omitempty"`
	Navigate *PageNavigation   `json:"navigate,omitempty"`
}

type PageActionEffect struct {
	Ref            AssetRef             `json:"ref"`
	RecordVariable string               `json:"recordVariable,omitempty"`
	Inputs         map[string]PageValue `json:"inputs,omitempty"`
}

type PageInterface struct {
	Version int                 `json:"version"`
	Inputs  map[string]PagePort `json:"inputs,omitempty"`
	Outputs map[string]PagePort `json:"outputs,omitempty"`
}

type PagePort struct {
	Variable string    `json:"variable"`
	Type     string    `json:"type"`
	Object   *AssetRef `json:"object,omitempty"`
	Required bool      `json:"required,omitempty"`
}

type PageNavigation struct {
	Page             AssetRef             `json:"page"`
	InterfaceVersion int                  `json:"interfaceVersion"`
	Inputs           map[string]PageValue `json:"inputs,omitempty"`
	Results          map[string]string    `json:"results,omitempty"`
}

const PageModelMaxFields = 64

type FieldRef struct {
	Name string   `json:"name,omitempty"`
	Path []string `json:"path,omitempty"`

	Slot      string `json:"slot,omitempty"`
	Transform string `json:"transform,omitempty"`
}

type ModelRelation struct {
	ParentType      string `json:"parentType"`
	ParentSelection string `json:"parentSelection,omitempty"`
	Field           string `json:"field"`
	Name            string `json:"name,omitempty"`
}

type ModelRecordSet struct {
	RecordVariable string        `json:"recordVariable,omitempty"`
	Name           string        `json:"name,omitempty"`
	Query          *AssetBinding `json:"query,omitempty"`
}

type ModelRule struct {
	Action AssetRef          `json:"action"`
	Inputs map[string]string `json:"inputs,omitempty"`
}

type OptionsRef struct {
	Set  *ModelRecordSet `json:"set,omitempty"`
	Rule *ModelRule      `json:"rule,omitempty"`
}

type ModelBinding struct {
	Type       string          `json:"type"`
	Fields     []FieldRef      `json:"fields,omitempty"`
	Relation   *ModelRelation  `json:"relation,omitempty"`
	Set        *ModelRecordSet `json:"set,omitempty"`
	Where      []Expr          `json:"where,omitempty"`
	Label      *Expr           `json:"label,omitempty"`
	Options    *OptionsRef     `json:"options,omitempty"`
	Visibility string          `json:"visibility,omitempty" enum:"hide,mark"`
}

type PageDataBinding struct {
	Value      *Expr                       `json:"value,omitempty"`
	Acyclic    bool                        `json:"acyclic,omitempty"`
	ID         string                      `json:"id"`
	Owner      string                      `json:"owner,omitempty"`
	Kind       string                      `json:"kind"`
	Object     AssetRef                    `json:"object"`
	Model      *ModelBinding               `json:"model,omitempty"`
	Models     map[string]ModelViewBinding `json:"models,omitempty"`
	Fields     []FieldRef                  `json:"fields,omitempty"`
	FieldSlots map[string][]FieldRef       `json:"fieldSlots,omitempty"`
	Ports      map[string]string           `json:"ports,omitempty"`
	Query      *AssetBinding               `json:"query,omitempty"`
	Resources  []PageResourceBinding       `json:"resources,omitempty"`
	Limit      int                         `json:"limit,omitempty"`
}

type PageResourceBinding struct {
	ReadOnly         bool                 `json:"readOnly,omitempty"`
	ID               string               `json:"id"`
	Kind             string               `json:"kind"`
	Asset            *AssetBinding        `json:"asset,omitempty"`
	Object           *AssetRef            `json:"object,omitempty"`
	URL              string               `json:"url,omitempty"`
	Origin           string               `json:"origin,omitempty"`
	ContentVersion   string               `json:"contentVersion,omitempty"`
	InterfaceVersion int                  `json:"interfaceVersion,omitempty"`
	Inputs           map[string]PageValue `json:"inputs,omitempty"`
	Outputs          map[string]string    `json:"outputs,omitempty"`
	Fields           []FieldRef           `json:"fields,omitempty"`
	Source           string               `json:"source,omitempty"`
	Limit            int                  `json:"limit,omitempty"`
}

type PageActionTrigger struct {
	ID         string                `json:"id"`
	Owner      string                `json:"owner,omitempty"`
	Binding    string                `json:"binding"`
	Actions    []AssetRef            `json:"actions,omitzero"`
	Function   *AssetBinding         `json:"function,omitempty"`
	Operation  *AssetBinding         `json:"operation,omitempty"`
	Inputs     map[string]Binding    `json:"inputs,omitempty"`
	Defaults   []PageActionParameter `json:"defaults,omitempty"`
	InlineEdit *PageInlineEdit       `json:"inlineEdit,omitempty"`
	Events     []PageEventBinding    `json:"events,omitempty"`
}

type PageInputSchema struct {
	profile    string
	Variables  map[string]PageVariable `json:"variables,omitempty"`
	Queries    map[string]PageQuery    `json:"queries,omitempty"`
	Interface  *PageInterface          `json:"interface,omitempty"`
	Selections []SelectionVariable     `json:"selections,omitempty"`
}

type PageScope struct {
	ID            string            `json:"id"`
	Parent        string            `json:"parent,omitempty"`
	Kind          string            `json:"kind"`
	Binding       string            `json:"binding,omitempty"`
	Bindings      map[string]string `json:"bindings,omitempty"`
	MaxIterations int               `json:"maxIterations,omitempty"`
	Inactive      bool              `json:"inactive,omitempty"`
}

type PageScopeVisibility struct {
	Scopes []PageScope       `json:"scopes"`
	Owners map[string]string `json:"owners"`
	Root   string            `json:"root"`
}

type PageWidgetSpec struct {
	Format string          `json:"format"`
	Body   json.RawMessage `json:"body"`
}

type ModelViewBinding struct {
	Model       ModelBinding        `json:"model"`
	Roles       map[string]FieldRef `json:"roles,omitempty"`
	Expressions map[string]Expr     `json:"expressions,omitempty"`
}

type PageExternalFrame struct {
	URL    string `json:"url"`
	Origin string `json:"origin"`
}

type PageActionParameter struct {
	Parameter string `json:"parameter"`
	Field     string `json:"field"`
}

type Page struct {
	Format            int                 `json:"format"`
	Name              string              `json:"name"`
	Title             string              `json:"title"`
	Description       string              `json:"description,omitempty"`
	Object            AssetRef            `json:"object"`
	DataBindings      []PageDataBinding   `json:"dataBindings"`
	ActionTriggers    []PageActionTrigger `json:"actionTriggers"`
	InputSchema       PageInputSchema     `json:"inputSchema"`
	ScopeVisibility   PageScopeVisibility `json:"scopeVisibility"`
	WidgetSpec        PageWidgetSpec      `json:"widgetSpec"`
	PresentationWords map[string]string   `json:"presentationWords,omitempty"`
	RetainedVersion   string              `json:"-"`
}

type SelectionVariable struct {
	Name   string   `json:"name"`
	Object AssetRef `json:"object"`
}

type PageInlineEdit struct {
	Action AssetRef `json:"action"`
	Fields []string `json:"fields"`
}

type PageQuery struct {
	Input     string        `json:"input,omitempty"`
	Direction string        `json:"direction,omitempty"`
	ItemOwner string        `json:"itemOwner,omitempty"`
	Set       *PageQuerySet `json:"set,omitempty"`
	Owner     string        `json:"owner,omitempty"`
	Title     string        `json:"title,omitempty"`
	Object    AssetRef      `json:"object"`
	Interface string        `json:"interface,omitempty"`
	Query     *AssetBinding `json:"query,omitempty"`
	Filter    *Expr         `json:"filter,omitempty"`

	Search *PageValue `json:"search,omitempty"`
	For    *PageValue `json:"for,omitempty"`
	Sort   []string   `json:"sort,omitempty"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset,omitempty"`
}

type PageQuerySet struct {
	Op     string   `json:"op"`
	Inputs []string `json:"inputs"`
}

type PageVariable struct {
	RuleType *ExprType           `json:"ruleType,omitempty"`
	Rule     *Expr               `json:"rule,omitempty"`
	Writable bool                `json:"writable,omitempty"`
	Title    string              `json:"title,omitempty"`
	Scope    string              `json:"scope"`
	Owner    string              `json:"owner,omitempty"`
	Type     string              `json:"type"`
	Mode     string              `json:"mode"`
	Initial  json.RawMessage     `json:"initial,omitempty"`
	Source   *PageResourceSource `json:"source,omitempty"`
}

type PageResourceSource struct {
	Compute *PageComputeResource `json:"compute,omitempty"`
	Port    string               `json:"port,omitempty"`
	Measure string               `json:"measure,omitempty"`
	Field   string               `json:"field,omitempty"`
	Fields  []string             `json:"fields,omitempty"`

	Object   *AssetRef `json:"object,omitempty"`
	Query    string    `json:"query,omitempty"`
	Variable string    `json:"variable,omitempty"`
	Kind     string    `json:"kind"`
	Section  string    `json:"section,omitempty"`
	Node     string    `json:"node,omitempty"`
}

type PageValue struct {
	Variable string          `json:"variable,omitempty"`
	Literal  json.RawMessage `json:"literal,omitempty"`
}
