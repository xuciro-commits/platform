package branches

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	"slices"
	"strconv"
	"strings"
	"time"
)

type BranchResourceStatus struct {
	Type    string    `json:"type"`
	ID      string    `json:"id"`
	RID     string    `json:"rid,omitempty"`
	Op      string    `json:"op" enum:"create,update,delete"`
	Changes int       `json:"changes"`
	LastSeq int       `json:"lastSeq"`
	LastAt  time.Time `json:"lastAt"`
	LastBy  string    `json:"lastBy"`
	Drifted bool      `json:"drifted,omitempty"`
}

type BranchDetail struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	State       string                 `json:"state" enum:"open,proposed,merged,closed"`
	Revision    uint32                 `json:"revision"`
	Created     platform.Stamp         `json:"created"`
	Changed     platform.Stamp         `json:"changed"`
	Changes     int                    `json:"changes"`
	Resources   []BranchResourceStatus `json:"resources"`
	Proposal    *build.BranchProposal  `json:"proposal,omitempty"`
	Check       *BranchCheck           `json:"check,omitempty"`
	CheckStale  bool                   `json:"checkStale,omitempty"`
	Merge       *build.BranchMerge     `json:"merge,omitempty"`
}

type BranchChangeEntry struct {
	Seq     int             `json:"seq"`
	At      time.Time       `json:"at"`
	By      string          `json:"by"`
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Project string          `json:"project,omitempty"`
	RID     string          `json:"rid,omitempty"`
	Op      string          `json:"op" enum:"create,update,delete"`
	Rebased bool            `json:"rebased,omitempty"`
	Base    json.RawMessage `json:"base,omitempty"`
	Spec    json.RawMessage `json:"spec,omitempty"`
}

type BranchResourceView struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Project string          `json:"project,omitempty"`
	RID     string          `json:"rid,omitempty"`
	Op      string          `json:"op,omitempty" enum:"create,update,delete"`
	Base    json.RawMessage `json:"base,omitempty"`
	Main    json.RawMessage `json:"main,omitempty"`
	Branch  json.RawMessage `json:"branch,omitempty"`
}

type BranchResourceDiff struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	RID       string          `json:"rid,omitempty"`
	Op        string          `json:"op" enum:"create,update,delete"`
	Branch    []PathChange    `json:"branch"`
	Main      []PathChange    `json:"main"`
	Conflicts []PathConflict  `json:"conflicts"`
	Merged    json.RawMessage `json:"merged,omitempty"`
}

type BranchDiff struct {
	Resources []BranchResourceDiff `json:"resources"`
	Conflicts int                  `json:"conflicts"`
}

type BranchResourceCheck struct {
	Type     string                `json:"type"`
	ID       string                `json:"id"`
	RID      string                `json:"rid,omitempty"`
	Action   string                `json:"action" enum:"create,edit,archive,none"`
	Artifact string                `json:"artifact,omitempty"`
	Accepted bool                  `json:"accepted"`
	Message  string                `json:"message,omitempty"`
	Code     string                `json:"code,omitempty"`
	Issues   []platform.FieldIssue `json:"issues,omitempty"`
}

type BranchGraphCheck struct {
	Passed    bool                `json:"passed"`
	Messages  []string            `json:"messages"`
	Candidate string              `json:"candidate,omitempty"`
	Added     []platform.AssetRef `json:"added,omitempty"`
	Removed   []platform.AssetRef `json:"removed,omitempty"`
	Changed   []platform.AssetRef `json:"changed,omitempty"`
}

type BranchSimulationCheck struct {
	CandidateID string   `json:"candidateId,omitempty"`
	Passed      *bool    `json:"passed,omitempty"`
	Steps       int      `json:"steps"`
	Messages    []string `json:"messages"`
}

type BranchCheck struct {
	At         time.Time              `json:"at"`
	By         string                 `json:"by"`
	Base       string                 `json:"base"`
	Content    string                 `json:"content"`
	Passed     bool                   `json:"passed"`
	Conflicts  int                    `json:"conflicts"`
	Resources  []BranchResourceCheck  `json:"resources"`
	Graph      BranchGraphCheck       `json:"graph"`
	Simulation *BranchSimulationCheck `json:"simulation,omitempty"`
}

type BranchCreateRequest struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type BranchSaveRequest struct {
	Op      string          `json:"op,omitempty" enum:"create,update,delete"`
	Project string          `json:"project,omitempty"`
	Spec    json.RawMessage `json:"spec,omitempty"`
}

type BranchResolveRequest struct {
	Resolutions []BranchResolution `json:"resolutions,omitempty"`
}

type BranchResolution struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Path string `json:"path"`
	Take string `json:"take" enum:"branch,main"`
}

type BranchReviewRequest struct {
	Decision string `json:"decision" enum:"approve,changes"`
	Note     string `json:"note,omitempty"`
}

type BranchConflictError struct {
	Conflicts []BranchResourceDiff `json:"conflicts"`
}

type MainDraft struct {
	Spec      json.RawMessage
	Revision  uint32
	Exists    bool
	RID       string
	Module    string
	BuildHash string
}

func Key(prefix, member, schema string, payload []byte, revision *uint32) string {
	sum := sha256.New()
	sum.Write([]byte(member + "\x00" + schema + "\x00"))
	sum.Write(payload)
	if revision != nil {
		sum.Write([]byte("\x00" + strconv.FormatUint(uint64(*revision), 10)))
	}
	return prefix + ":" + hex.EncodeToString(sum.Sum(nil))[:24]
}

const CodeBuildKey = "build"

func KeptCodeBuild(previous build.BranchChange, touched bool, spec map[string]json.RawMessage) json.RawMessage {
	if !touched || previous.Op == "delete" {
		return nil
	}
	var earlier map[string]json.RawMessage
	if json.Unmarshal(previous.Spec, &earlier) != nil || earlier[CodeBuildKey] == nil {
		return nil
	}
	var artifact build.CodeArtifact
	authored, _ := json.Marshal(spec)
	if json.Unmarshal(earlier[CodeBuildKey], &artifact) != nil || !artifact.Matches(authored) {
		return nil
	}
	return earlier[CodeBuildKey]
}

func EffectiveSpec(branch build.Branch, typ, id string) (spec json.RawMessage, deleted bool) {
	latest, ok := branch.Latest(typ, id)
	if !ok || latest.Op == "delete" {
		return nil, ok
	}
	return latest.Spec, false
}

type ResourcePlan struct {
	Type, ID string
	RID      string
	Project  string
	Op       string
	Base     json.RawMessage
	Main     MainDraft
	Branch   json.RawMessage
	Merged   json.RawMessage
	Diff     BranchResourceDiff
}

func ResolutionsFor(requested []BranchResolution, typ, id string) []Resolution {
	var out []Resolution
	for _, r := range requested {
		if r.Type == typ && r.ID == id {
			out = append(out, Resolution{Path: r.Path, Take: r.Take})
		}
	}
	return out
}

func ConflictsOf(plans []ResourcePlan) []BranchResourceDiff {
	out := []BranchResourceDiff{}
	for _, plan := range plans {
		if len(plan.Diff.Conflicts) > 0 {
			out = append(out, plan.Diff)
		}
	}
	return out
}

type Action struct {
	Plan     ResourcePlan
	Action   string
	Schema   string
	Payload  map[string]json.RawMessage
	Artifact json.RawMessage
}

func (a Action) Module() string {
	var artifact build.CodeArtifact
	_ = json.Unmarshal(a.Artifact, &artifact)
	return artifact.Module
}

func (a Action) Submission(tenant string, m platform.Member, key string) *pb.Submission {
	payload, _ := json.Marshal(a.Payload)
	s := &pb.Submission{TenantId: tenant, PrincipalId: m.ID, Authority: build.ID,
		Target: &pb.EntityRef{Type: a.Plan.Type, Id: a.Plan.ID}, Schema: &pb.SchemaRef{Name: a.Schema, Version: 1}, Payload: payload}
	if a.Action != "create" && a.Plan.Main.Revision > 0 {
		revision := a.Plan.Main.Revision
		s.ExpectedRevision = &revision
	}
	s.IdempotencyKey = Key(key, m.ID, a.Schema, payload, s.ExpectedRevision)
	return s
}

func SameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	left, _ := json.Marshal(x)
	right, _ := json.Marshal(y)
	return string(left) == string(right)
}

func DecodeCheck(branch build.Branch) *BranchCheck {
	if len(branch.Check) == 0 {
		return nil
	}
	var check BranchCheck
	if json.Unmarshal(branch.Check, &check) != nil {
		return nil
	}
	if check.Resources == nil {
		check.Resources = []BranchResourceCheck{}
	}
	if check.Graph.Messages == nil {
		check.Graph.Messages = []string{}
	}
	return &check
}

func (c BranchCheck) Failure() string {
	for _, resource := range c.Resources {
		if !resource.Accepted {
			return resource.Type + "/" + resource.ID + ": " + resource.Message
		}
	}
	if len(c.Graph.Messages) > 0 && !c.Graph.Passed {
		return c.Graph.Messages[0]
	}
	if c.Simulation != nil && len(c.Simulation.Messages) > 0 && (c.Simulation.Passed == nil || !*c.Simulation.Passed) {
		return c.Simulation.Messages[0]
	}
	if c.Conflicts > 0 {
		return strconv.Itoa(c.Conflicts) + " resources conflict with main"
	}
	return ""
}

func Content(plans []ResourcePlan) string {
	ordered := slices.Clone(plans)
	slices.SortFunc(ordered, func(a, b ResourcePlan) int { return strings.Compare(a.Type+"/"+a.ID, b.Type+"/"+b.ID) })
	sum := sha256.New()
	for _, plan := range ordered {
		sum.Write([]byte(plan.Type + "\x00" + plan.ID + "\x00" + plan.Op + "\x00" + plan.Project + "\x00"))
		sum.Write(plan.Merged)
		sum.Write([]byte("\x00"))
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func DraftSet(owner build.ConstructionDrafts, actions []Action) []build.JointDraftRef {
	var drafts []build.JointDraftRef
	for _, item := range actions {
		if item.Action == "none" || item.Action == "archive" {
			continue
		}
		ref, err := build.BranchResource(item.Plan.Type, item.Plan.ID)
		if err != nil {
			continue
		}
		selected := []build.JointDraftRef{{Kind: ref.Kind, ID: item.Plan.ID}}
		if closure, err := owner.ReferencedDrafts(ref.Kind, item.Plan.ID, ""); err == nil {
			selected = append(selected, closure...)
		}
		for _, draft := range selected {
			if !slices.Contains(drafts, draft) {
				drafts = append(drafts, draft)
			}
		}
	}
	return drafts
}

func (s *BranchSimulationCheck) OK() bool {
	return s == nil || len(s.Messages) == 0 && (s.Passed == nil || *s.Passed)
}
