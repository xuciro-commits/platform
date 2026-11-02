package work

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const SchemaTaskCreate = TaskType + ".create"

func taskActions() []platform.Action {
	return []platform.Action{{Schema: SchemaTaskCreate, Target: TaskType, New: true, Title: "Create work item", Description: "Create a work item on a readable record and notify its assignee or candidates.", Capability: "tasks", Roles: []string{platform.AnyMember}, Payload: []platform.Field{
		{Name: "title", Type: "string", Required: true}, {Name: "body", Type: "string"}, {Name: "ref", Type: "string", Required: true}, {Name: "assignee", Type: "string"}, {Name: "candidates", Type: "json"}, {Name: "due", Type: "datetime"},
	}}}
}

func (w *Work) createTask(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var input struct {
		Title      string    `json:"title"`
		Body       string    `json:"body"`
		Ref        string    `json:"ref"`
		Assignee   string    `json:"assignee"`
		Candidates []string  `json:"candidates"`
		Due        time.Time `json:"due"`
	}
	if json.Unmarshal(s.GetPayload(), &input) != nil || strings.TrimSpace(input.Title) == "" || !strings.Contains(input.Ref, "/") {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A work item needs a title and a subject record")
	}
	task := WorkTask{Record: platform.Record{ID: s.GetTarget().GetId()}, Title: input.Title, Body: input.Body, Ref: input.Ref, Assignee: input.Assignee, Candidates: input.Candidates, Due: input.Due, State: "open"}
	typ, id, _ := strings.Cut(task.Ref, "/")
	if _, refusal := c.ReadRecord(typ, id, now); refusal != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Choose a record you can read")
	}
	if _, exists := platform.Get[WorkTask](c, s.GetTarget().GetId()); exists {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The work item already exists")
	}
	if task.Assignee != "" {
		task.Candidates = append(task.Candidates, task.Assignee)
	}
	slices.Sort(task.Candidates)
	task.Candidates = slices.Compact(task.Candidates)
	if len(task.Candidates) == 0 {
		task.Candidates = []string{c.ID}
	}
	for _, id := range task.Candidates {
		member, exists := w.host.Member(id)
		if !exists || !member.Active() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an active assignee or candidate")
		}
	}
	task.App, _, _ = strings.Cut(task.Ref, ".")
	return func(r *pb.ChangeRecord) {
		c.Put(r, task)
		recipients := []platform.Recipient{}
		for _, id := range task.Candidates {
			recipients = append(recipients, platform.Recipient{Member: id})
		}
		c.Notify(platform.Notification{Title: task.Title, Body: task.Body, Ref: TaskType + "/" + task.ID, Key: "task:" + task.ID}, now, recipients...)
	}, nil
}

func (w *Work) cancelTask(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	task := record.(*WorkTask)
	if strings.HasPrefix(task.Ref, ApprovalType+"/") {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Approval work items close with their approval request")
	}
	if task.Created.By != c.ID && task.Assignee != c.ID {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the creator or assignee can cancel this work item")
	}
	return nil
}
