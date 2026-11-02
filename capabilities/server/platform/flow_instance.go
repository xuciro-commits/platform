package platform

import "encoding/json"

type FlowInstance struct {
	Record
	Applications    []AssetBinding             `json:"applications,omitempty" field:"readonly" type:"json"`
	Flow            string                     `json:"flow" field:"readonly,search"`
	Title           string                     `json:"title" field:"readonly,search"`
	Version         int                        `json:"version" field:"readonly"`
	Dependencies    string                     `json:"dependencies,omitempty" field:"readonly" title:"Dependency release"`
	Release         string                     `json:"release,omitempty" field:"readonly" title:"Active release"`
	Key             string                     `json:"key" field:"readonly,search"`
	Subject         string                     `json:"subject,omitempty" field:"readonly"`
	State           string                     `json:"state" field:"readonly" choices:"running,waiting,done,compensating,compensated,canceled,stuck"`
	OnBehalf        string                     `json:"onBehalf,omitempty" field:"readonly" title:"On behalf of"`
	Data            string                     `json:"data,omitempty" field:"readonly" type:"longtext" source:"external-input"`
	Outputs         map[string]json.RawMessage `json:"outputs,omitempty" field:"readonly" type:"json"`
	Sources         []string                   `json:"sources,omitempty" field:"readonly"`
	Withheld        bool                       `json:"withheld,omitempty" field:"readonly"`
	Answer          string                     `json:"answer,omitempty" field:"readonly"`
	Parent          string                     `json:"parent,omitempty" field:"readonly"`
	Tokens          []FlowToken                `json:"tokens" field:"readonly" title:"Where it stands"`
	Batch           *FlowBatchFrame            `json:"batch,omitempty" field:"readonly" type:"json"`
	ArtifactCleanup []FlowStateArtifact        `json:"artifactCleanup,omitempty" field:"readonly" type:"json"`
	CleanupPending  bool                       `json:"cleanupPending,omitempty" field:"readonly"`
	Undo            []FlowUndoEntry            `json:"undo" field:"readonly" title:"To undo"`
	Trace           []FlowTraceLine            `json:"trace" field:"readonly"`
	Seq             int                        `json:"seq" field:"readonly"`
	Resumable       bool                       `json:"resumable,omitempty" field:"readonly"`
}

type FlowOutput struct {
	Record
	App      string          `json:"app" field:"readonly"`
	Instance string          `json:"instance" field:"readonly"`
	Flow     string          `json:"flow" field:"readonly"`
	Version  int             `json:"version" field:"readonly"`
	Name     string          `json:"name" field:"readonly"`
	Batch    string          `json:"batch" field:"readonly"`
	OnBehalf string          `json:"onBehalf" field:"readonly"`
	Sources  []string        `json:"sources,omitempty" field:"readonly"`
	Value    json.RawMessage `json:"value" field:"readonly" type:"json"`
	Withheld bool            `json:"withheld,omitempty" field:"readonly"`
}
