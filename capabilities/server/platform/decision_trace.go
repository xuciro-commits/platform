package platform

import "encoding/json"

// DecisionTrace is an observation of the original executor in an isolated
// builder run. It never participates in acceptance, grants or persistence.
type DecisionTrace struct {
	Kind          string          `json:"kind"`
	Name          string          `json:"name,omitempty"`
	Type          string          `json:"type,omitempty"`
	ID            string          `json:"id,omitempty"`
	Operation     string          `json:"operation,omitempty"`
	Expression    *Expr           `json:"expression,omitempty"`
	Reads         *ExprContext    `json:"reads,omitempty"`
	Result        *ExprResult     `json:"result,omitempty"`
	Request       *AuthzRequest   `json:"request,omitempty"`
	Authorization *AuthzDecision  `json:"authorization,omitempty"`
	Before        json.RawMessage `json:"before,omitempty"`
	After         json.RawMessage `json:"after,omitempty"`
	Refusal       string          `json:"refusal,omitempty"`
}
type DecisionTraceRecorder interface {
	TraceEnabled() bool
	RecordDecisionTrace(DecisionTrace)
}

func (c Caller) TraceEnabled() bool {
	r, ok := c.rt.(DecisionTraceRecorder)
	return ok && r.TraceEnabled()
}
func (c Caller) Trace(event DecisionTrace) {
	if r, ok := c.rt.(DecisionTraceRecorder); ok && r.TraceEnabled() {
		r.RecordDecisionTrace(event)
	}
}

// Only references reached by the real expression belong in its explanation.
// The original RuleContext has already enforced record/field/derived reads.
func ruleTraceContext(expr Expr, context ExprContext) ExprContext {
	out := ExprContext{Refs: map[string]ExprValue{}, Sets: map[string]ExprCollection{}, Sources: context.Sources, Me: context.Me, Now: context.Now, Today: context.Today}
	var visit func(Expr)
	visit = func(e Expr) {
		if e.Ref != "" {
			if v, ok := context.Refs[e.Ref]; ok {
				out.Refs[e.Ref] = v
			}
		}
		if e.Agg != nil {
			out.Sets[e.Agg.Source] = context.Sets[e.Agg.Source]
		}
		for _, arg := range e.Args {
			visit(arg)
		}
		if e.In != nil {
			visit(*e.In)
		}
	}
	visit(expr)
	return out
}
