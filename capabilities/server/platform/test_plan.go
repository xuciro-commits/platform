package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// TestPlanDescriptor is a portable business scenario. Roots identify owner
// definitions, never local editor rows, application IDs or tenant members.
type TestPlanDescriptor struct {
	Name    string          `json:"name"`
	Title   string          `json:"title"`
	Roots   []AssetRef      `json:"roots"`
	At      time.Time       `json:"at"`
	Samples []SampleRecords `json:"samples,omitempty"`
	Steps   []TestStep      `json:"steps"`
}

func (p TestPlanDescriptor) Check() error {
	if !sampleSetName.MatchString(p.Name) || p.Title == "" || len(p.Title) > 256 || p.At.IsZero() || len(p.Roots) < 1 || len(p.Roots) > 16 || len(p.Steps) < 1 || len(p.Steps) > 20 {
		return fmt.Errorf("a test plan needs a name, title, definition roots, fixed time and 1–20 steps")
	}
	seen := map[AssetRef]bool{}
	for _, root := range p.Roots {
		if root.Check() != nil || root.App != "build" || root.Kind != AssetObject && root.Kind != AssetFlow || seen[root] {
			return fmt.Errorf("test roots must be distinct Build objects or workflows")
		}
		seen[root] = true
	}
	if err := (SampleSet{Name: p.Name, Title: p.Title, Samples: p.Samples}).Check(); err != nil {
		return err
	}
	for _, step := range p.Steps {
		if step.As != "" {
			return fmt.Errorf("portable test steps use the member chosen for the run")
		}
		if err := step.Check(); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > 512<<10 {
		return fmt.Errorf("test plan exceeds its byte budget")
	}
	return nil
}

func ReadTestPlan(raw []byte) (TestPlanDescriptor, error) {
	var p TestPlanDescriptor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 512<<10 || decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF {
		return p, fmt.Errorf("invalid test plan descriptor")
	}
	return p, p.Check()
}

func (p TestPlanDescriptor) Dependencies() []AssetRef {
	refs := append([]AssetRef{}, p.Roots...)
	for _, sample := range p.Samples {
		refs = append(refs, ModelObject(sample.Type))
	}
	for _, step := range p.Steps {
		if step.Action != "" && step.Type != "build.process" {
			refs = append(refs, AssetRef{App: "build", Kind: AssetAction, Name: step.Action})
		}
		if step.Flow != "" {
			refs = append(refs, AssetRef{App: "build", Kind: AssetFlow, Name: step.Flow})
		}
	}
	return refs
}

type TestStep struct {
	Read           bool             `json:"read,omitempty"`
	Type           string           `json:"type"`
	ID             string           `json:"id"`
	Action         string           `json:"action"`
	Payload        string           `json:"payload"`
	Expect         string           `json:"expect" title:"Expected outcome"`
	As             string           `json:"as,omitempty" title:"Step member ID (empty: plan member)"`
	AdvanceSeconds int              `json:"advanceSeconds,omitempty" title:"Advance clock (seconds)"`
	Flow           string           `json:"flow,omitempty"`
	Step           string           `json:"step,omitempty"`
	Answer         string           `json:"answer,omitempty"`
	Function       *FunctionFixture `json:"function,omitempty"`
	Compute        *ComputeFixture  `json:"compute,omitempty" type:"json"`
}

func (s TestStep) Check() error {
	var payload map[string]json.RawMessage
	if !strings.HasPrefix(s.Type, "build.") || s.Type == "build.object" || s.Type == "build.page" || s.Type == "build.app" || s.Type == "build.testplan" || s.Type == "build.sample-set" || s.Type == "build.process" && s.Action != "build.process.run" && s.AdvanceSeconds == 0 || s.Type == "build.function" || strings.TrimSpace(s.ID) == "" || s.AdvanceSeconds < 0 || s.AdvanceSeconds > 86400 || s.Expect != "accepted" && s.Expect != "refused" || json.Unmarshal([]byte(s.Payload), &payload) != nil || payload == nil {
		return fmt.Errorf("Every test step needs a candidate action or answer, record ID, JSON object, bounded time and expected outcome")
	}
	if s.Compute != nil && !s.Compute.Check() {
		return fmt.Errorf("Use a bounded compute fixture with a completed or failed outcome")
	}
	if s.Function != nil && !s.Function.Check() {
		return fmt.Errorf("Use a bounded function fixture with a ready or rejected outcome")
	}
	if s.Read {
		if s.Action != "" || s.Answer != "" || s.Flow != "" || s.Step != "" || s.AdvanceSeconds != 0 {
			return fmt.Errorf("A read step needs only an object and record ID")
		}
	} else if s.Answer == "" {
		if (s.Action == "" && s.AdvanceSeconds == 0 || s.Action != "" && !strings.HasPrefix(s.Action, s.Type+".")) || s.Flow != "" || s.Step != "" {
			return fmt.Errorf("Choose one candidate action or workflow answer per test step")
		}
	} else if s.Action != "" || !strings.HasPrefix(s.Flow, "build.") || !sampleSetName.MatchString(s.Step) {
		return fmt.Errorf("Choose one candidate action or workflow answer per test step")
	}
	return nil
}

type ComputeFixture struct {
	App          string `json:"app,omitempty"`
	Name         string `json:"name"`
	Output       string `json:"output,omitempty"`
	Error        string `json:"error,omitempty"`
	ExpectState  string `json:"expectState" enum:"completed,failed"`
	ExpectOutput string `json:"expectOutput,omitempty"`
}

func (f ComputeFixture) Check() bool {
	return f.Name != "" && len(f.Name) <= 64 && len(f.Output) <= 48<<10 && len(f.Error) <= 1024 && len(f.ExpectOutput) <= 48<<10 && (f.ExpectState == "completed" && json.Valid([]byte(f.Output)) || f.ExpectState == "failed" && f.Error != "") && (f.ExpectOutput == "" || json.Valid([]byte(f.ExpectOutput)))
}

type FunctionFixture struct {
	Output       string `json:"output"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
	ExpectState  string `json:"expectState" enum:"ready,rejected"`
	ExpectOutput string `json:"expectOutput,omitempty"`
}

func (f FunctionFixture) Check() bool {
	return len(f.Output) <= 64<<10 && len(f.ExpectOutput) <= 32<<10 && f.InputTokens >= 0 && f.InputTokens <= 1<<20 && f.OutputTokens >= 0 && f.OutputTokens <= 1<<20 && (f.ExpectState == "ready" || f.ExpectState == "rejected") && (f.ExpectOutput == "" || f.ExpectState == "ready" && json.Valid([]byte(f.ExpectOutput)))
}
