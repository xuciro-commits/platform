package work

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
)

const (
	QuestionType          = runtimeprotocol.WorkQuestionType
	SchemaQuestionRequest = runtimeprotocol.WorkQuestionRequestSchema
)

type AgentQuestion = runtimeprotocol.AgentQuestion

func (w *Work) questionEntity() platform.RecordType {
	everyone := []string{platform.AnyMember}
	return platform.RecordType{
		Type: QuestionType, Title: "Question", Model: AgentQuestion{},
		Scope: platform.Scope{Participants: func(record any) []string {
			q := record.(AgentQuestion)
			return append([]string{q.Requester, q.AnsweredBy}, q.Candidates...)
		}},
		Lifecycle: &platform.Lifecycle{
			Field: "state", Initial: "open",
			States: []platform.State{
				{Name: "open", Title: "Open", Tone: "warning"},
				{Name: "approved", Title: "Approved", Tone: "success"},
				{Name: "rejected", Title: "Rejected", Tone: "danger"},
				{Name: "answered", Title: "Answered", Tone: "success"},
				{Name: "expired", Title: "Expired", Tone: "neutral"},
				{Name: "refused", Title: "Refused when run", Tone: "danger"},
			},
			Actions: []platform.ActionType{{
				Name: "answer", Title: "Answer", Roles: everyone, Capability: "approvals",
				Description: "Answer a question from an agent; approving an ask-first action runs it once with the original caller and parameters.",
				Parameters:  []platform.Field{{Name: "choice", Type: "string", Required: true, Description: "Chosen option"}, {Name: "note", Type: "string", Description: "Why"}},
				Criteria:    platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"open"}}},
				Rules: []platform.ActionRule{
					{Function: w.decideQuestion},
					{Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"open", "approved", "rejected", "answered", "expired", "refused"}}},
					{Receipt: w.finishQuestion},
				},
			}},
		},
	}
}

func (w *Work) RequestQuestion(c platform.Caller, held *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	if c.Automation || c.Tenant == "" || held.GetTenantId() != c.Tenant || held.GetPrincipalId() != c.ID || held.GetAuthority() != c.App {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The question needs its original authorised member")
	}
	raw, err := protojson.Marshal(held)
	if err != nil {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	payloadMap := map[string]any{
		"requester":   c.ID,
		"submission":  json.RawMessage(raw),
		"scopes":      c.Scopes,
		"agentCaller": c.Agent,
	}
	if caller := runtimeprotocol.ReadAICaller(c.Member); caller != nil {
		payloadMap["caller"] = caller
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	id := c.App + "." + held.GetIdempotencyKey()
	automation := w.host.Automation(c, ID)
	request := &pb.Submission{
		TenantId: c.Tenant, PrincipalId: automation.ID, Authority: ID,
		IdempotencyKey: "question:" + id, Target: &pb.EntityRef{Type: QuestionType, Id: id},
		Schema: &pb.SchemaRef{Name: SchemaQuestionRequest, Version: 1}, Payload: payload,
	}
	return platform.Decide(automation, w, request, now)
}

func (w *Work) questionRequest(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *platform.Error) {
	invalid := &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	var p struct {
		Requester   string                    `json:"requester"`
		Submission  json.RawMessage           `json:"submission"`
		Scopes      []string                  `json:"scopes"`
		AgentCaller bool                      `json:"agentCaller"`
		Caller      *runtimeprotocol.AICaller `json:"caller"`
	}
	held := &pb.Submission{}
	if json.Unmarshal(s.GetPayload(), &p) != nil || protojson.Unmarshal(p.Submission, held) != nil {
		return nil, invalid
	}
	app, declared, known := w.host.Action(held.GetSchema().GetName())
	requester, ok := w.host.Member(p.Requester)
	if !known || !declared.Ask || !ok {
		return nil, invalid
	}
	var due time.Time
	if p.Caller != nil && !p.Caller.Due.IsZero() {
		due = p.Caller.Due
	}
	delegates := w.delegates(c, []string{requester.ID}, now)
	candidates := []string{requester.ID}
	for delegate := range delegates {
		candidates = append(candidates, delegate)
	}
	slices.Sort(candidates)
	candidates = slices.Compact(candidates)
	release, refusal := w.host.ApprovalRelease(app, declared.Schema, held.GetTarget().GetType())
	if refusal != nil {
		return nil, refusal
	}
	target := held.GetTarget().GetType()
	if held.GetTarget().GetId() != "" {
		target += "/" + held.GetTarget().GetId()
	}
	raw, _ := protojson.Marshal(held)
	question := AgentQuestion{
		Record:       platform.Record{ID: s.GetTarget().GetId()},
		Action:       declared.Schema,
		Title:        declared.Title,
		Reason:       declared.AskReason,
		App:          app,
		Target:       target,
		Payload:      string(held.GetPayload()),
		Requester:    requester.ID,
		Candidates:   candidates,
		Scopes:       slices.Clone(p.Scopes),
		AgentCaller:  p.AgentCaller,
		Caller:       p.Caller,
		Submission:   string(raw),
		Options:      []string{"approve", "reject"},
		OptionTitles: []string{"Approve", "Reject"},
		Recommend:    0,
		Due:          due,
		State:        "open",
		Release:      release,
	}
	if _, exists := platform.Get[AgentQuestion](c, question.ID); exists {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return func(r *pb.ChangeRecord) {
		c.Put(r, question)
		w.offerQuestion(c, r, question, cmp.Or(declared.AskReason, declared.Description), now)
	}, nil
}

func (w *Work) offerQuestion(c platform.Caller, r *pb.ChangeRecord, q AgentQuestion, body string, now time.Time) {
	task := WorkTask{
		Record: platform.Record{ID: "question:" + q.ID}, Title: q.Title,
		Body: body, Ref: QuestionType + "/" + q.ID, App: q.App,
		Candidates: q.Candidates, Assignee: q.Requester, Due: q.Due, State: "open", Answers: slices.Clone(q.Options),
	}
	c.Put(r, task)
	to := make([]platform.Recipient, 0, len(q.Candidates))
	for _, m := range q.Candidates {
		to = append(to, platform.Recipient{Member: m})
	}
	c.Notify(platform.Notification{Category: runtimeprotocol.NotificationAssignment, Title: task.Title, Body: task.Body, Ref: task.Ref, Key: "task:" + task.ID}, now, to...)
}

func (w *Work) closeQuestionTask(c platform.Caller, r *pb.ChangeRecord, qID, state string) {
	if t, ok := platform.Get[WorkTask](c, "question:"+qID); ok && t.State == "open" {
		t.State = state
		c.Put(r, t)
		w.closed(c, t.ID)
	}
}

func (w *Work) decideQuestion(c platform.Caller, record any, payload json.RawMessage, now time.Time) *platform.Error {
	q := record.(*AgentQuestion)
	if c.IsAI() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A person must answer an agent question in the inbox")
	}
	if c.Automation && !q.Due.IsZero() && now.After(q.Due) {
		q.State = "expired"
		return nil
	}
	if c.ID != q.Requester && !slices.Contains(q.Candidates, c.ID) {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if !q.Due.IsZero() && now.After(q.Due) {
		q.State = "expired"
		return nil
	}
	var p struct {
		Choice string `json:"choice"`
		Note   string `json:"note"`
	}
	if json.Unmarshal(payload, &p) != nil || !slices.Contains(q.Options, p.Choice) {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	q.Answer, q.AnsweredBy, q.AnsweredAt, q.Outcome = p.Choice, c.ID, now, strings.TrimSpace(p.Note)
	if q.Submission == "" {
		q.State = "answered"
	} else if p.Choice == "approve" {
		q.State = "approved"
	} else {
		q.State = "rejected"
	}
	return nil
}

func (w *Work) finishQuestion(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
	q := *record.(*AgentQuestion)
	if q.State == "expired" || q.State == "rejected" {
		w.closeQuestionTask(c, r, q.ID, "canceled")
		c.Put(r, q)
		return
	}
	w.closeQuestionTask(c, r, q.ID, "done")
	if q.Submission == "" {
		c.Put(r, q)
		return
	}
	held := &pb.Submission{}
	_ = protojson.Unmarshal([]byte(q.Submission), held)
	held.ExpectedRevision = nil
	execution, err := w.host.ApprovalExecution(c, ApprovalRequest{Release: q.Release, App: q.App, Action: q.Action, Target: q.Target})
	if err == nil {
		requester, ok := execution.Member(q.Requester)
		if !ok {
			err = &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		} else {
			requester.Scopes = slices.Clone(q.Scopes)
			requester.Agent = q.AgentCaller
			if q.Caller != nil {
				cp := *q.Caller
				requester = runtimeprotocol.WithAICaller(requester, &cp)
			}
			requester.AIContext = true
			requester.AskOK = true
			_, err = execution.Attempt(execution.Caller(c, requester, q.App), held, now)
		}
	}
	if err != nil {
		q.State, q.Outcome = "refused", err.Error()
		if err.Message != "" {
			q.Outcome += ": " + err.Message
		}
	} else {
		q.State = "approved"
	}
	c.Put(r, q)
	c.Notify(platform.Notification{
		Category: runtimeprotocol.NotificationSystem,
		Title:    fmt.Sprintf("%s %s: %s", q.Title, q.Target, q.State),
		Body:     q.Outcome,
		Ref:      QuestionType + "/" + q.ID,
		Key:      "question:" + q.ID,
	}, now, platform.Recipient{Member: q.Requester})
}

func (w *Work) expireQuestions(c platform.Caller, now time.Time) {
	open, _ := json.Marshal([]any{[]any{"state", "=", "open"}})
	questions, _, _ := platform.Find[AgentQuestion](c, platform.Query{Domain: open})
	for _, q := range questions {
		if q.Due.IsZero() || !now.After(q.Due) {
			continue
		}
		_, _ = platform.Decide(c, w, &pb.Submission{
			TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID,
			IdempotencyKey: "question-expire:" + q.ID, Target: &pb.EntityRef{Type: QuestionType, Id: q.ID},
			Schema: &pb.SchemaRef{Name: QuestionType + ".answer", Version: 1}, Payload: []byte(`{"choice":"reject"}`),
		}, now)
	}
}
