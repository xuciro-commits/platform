package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
)

func (b *Build) openDispatch(c platform.Caller, receipt *pb.ChangeRecord, txn *platform.BusinessTxn, scope platform.RuleReadSpec, subject map[string]any, values map[string]any, at time.Time) *kernel.Error {
	if txn.Dispatch == nil {
		return nil
	}
	transport := c.DispatchTransport()
	if transport == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Dispatch transport is unavailable")
	}
	if c.Simulating() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Device dispatch does not execute in isolated trials")
	}
	spec := *txn.Dispatch
	release, assets, err := transport.FreezeDispatch(c, txn.Subject, receipt.GetSubmission().GetSchema().GetName(), spec, at)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	var object Object
	for _, asset := range assets {
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetObject && asset.Ref.Name == txn.Subject {
			if err := json.Unmarshal(asset.Body, &object); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Dispatch subject publication is invalid")
			}
			break
		}
	}
	if err := mapDispatchIntegrations(&object, b.data.IntegrationID); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	command, known := dispatchCommand(object, spec.Command)
	if !known {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Dispatch command is absent from its frozen publication")
	}
	key := receipt.GetSubmission().GetTarget().GetId()
	if spec.Key != "$subject" {
		raw, _, problem := c.ReadRecordPath(txn.Subject, key, strings.Split(strings.TrimPrefix(spec.Key, "record."), "."), at)
		if problem != nil {
			return problem
		}
		if json.Unmarshal(raw, &key) != nil || strings.TrimSpace(key) == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Dispatch business key is missing")
		}
	}
	digest := sha256.Sum256([]byte(txn.Subject + ":" + command.Name + ":" + key))
	id := "dispatch:" + hex.EncodeToString(digest[:16])
	if _, exists := platform.Get[platform.DispatchRun](c, id); exists {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This business key already has a dispatch; review the existing command")
	}
	payload := map[string]any{}
	for _, set := range spec.Payload {
		expr, err := platform.DispatchParameter(set, scope)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		value, problem := platform.RuleAssignment(expr, scope, c, subject, values, at, b.conditionLookup)
		if problem != nil {
			return problem
		}
		payload[set.Field] = value
	}
	payload["key"] = key
	run := platform.DispatchRun{Record: platform.Record{ID: id}, Subject: txn.Subject + "/" + receipt.GetSubmission().GetTarget().GetId(), Action: receipt.GetSubmission().GetSchema().GetName(), Member: c.ID, Command: command, Definition: spec, Target: spec.Target, Key: key, State: "pending", StartedAt: at, StateAt: at, Deadline: at.Add(time.Duration(spec.DeadlineSeconds) * time.Second), ReadDue: at, Release: release, Assets: assets, Input: platform.Raw(payload), Readback: []platform.DispatchReading{}, Attempts: []platform.DispatchAttempt{}, Reviewers: []string{}}
	run.Reviewers = append(run.Reviewers, b.host.(platform.ConfirmationHost).Holding(ID, spec.ReviewerRole)...)
	connection := func(id string) (dataowner.Connection, error) {
		conn, found := platform.Get[dataowner.Connection](c, id)
		if !found {
			return dataowner.Connection{}, fmt.Errorf("Dispatch connection is unavailable")
		}
		return b.data.ExecutionConnection(conn)
	}
	prepare := func(command platform.DispatchCommand) (json.RawMessage, error) {
		w, found := platform.Get[dataowner.Writeback](c, command.Writeback)
		if !found {
			return nil, fmt.Errorf("Dispatch writeback is unavailable")
		}
		w, err = b.data.ExecutionWriteback(w)
		if err != nil {
			return nil, err
		}
		conn, err := connection(spec.Target)
		if err != nil {
			return nil, err
		}
		requestInput := run.Input
		if command.Name != run.Command.Name {
			reverseInput := make(map[string]any, len(payload))
			for name, value := range payload {
				reverseInput[name] = value
			}
			reverseInput["key"] = run.Key + ":compensate"
			requestInput = platform.Raw(reverseInput)
		}
		req, err := w.Request(conn, receipt.GetSubmission().GetTarget().GetId(), requestInput)
		if err != nil {
			return nil, err
		}
		req.Dispatch, req.SafeRetry = id, command.SafeRetry()
		return platform.Raw(req), nil
	}
	run.Request, err = prepare(command)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
	}
	if command.Compensate != "" {
		reverse, _ := dispatchCommand(object, command.Compensate)
		run.CompensationCommand = &reverse
		run.CompensationRequest, err = prepare(reverse)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
	}
	if spec.Readback.Source != "" {
		source, found := platform.Get[dataowner.Source](c, spec.Readback.Source)
		if !found {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Dispatch status source is unavailable")
		}
		source, err = b.data.ExecutionSource(source)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		conn, err := connection(source.Connection)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		source.Cursor = ""
		run.Source, run.Connection = platform.Raw(source), platform.Raw(conn)
	}
	if spec.Readback.Observation != "" {
		observation, found := platform.Get[dataowner.Observation](c, spec.Readback.Observation)
		if !found {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Dispatch observation is unavailable")
		}
		definition, err := b.data.ExecutionObservation(observation)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		conn, err := connection(observation.Connection)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
		}
		device, _, problem := c.ReadRecordPath(txn.Subject, receipt.GetSubmission().GetTarget().GetId(), []string{definition.DeviceField}, at)
		if problem != nil {
			return problem
		}
		if json.Unmarshal(device, &run.Device) != nil || run.Device == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Dispatch observation device is missing")
		}
		run.Observation, run.Connection = platform.Raw(definition), platform.Raw(conn)
	}
	if err := b.queueDispatch(c, receipt, &run, at); err != nil {
		return err
	}
	return c.Put(receipt, run)
}

func (b *Build) queueDispatch(c platform.Caller, receipt *pb.ChangeRecord, run *platform.DispatchRun, at time.Time) *kernel.Error {
	run.Attempt++
	run.Intent = fmt.Sprintf("%s:%s:dispatch:%s:%d:%s%s", c.Tenant, ID, run.ID, run.Attempt, dataowner.WritebackEndpoint, run.Target)
	run.Attempts = append(run.Attempts, platform.DispatchAttempt{Number: run.Attempt, Intent: run.Intent, Target: run.Target, State: "pending", At: at, Compensation: run.Compensating})
	if len(run.Attempts) > 20 {
		run.Attempts = run.Attempts[len(run.Attempts)-20:]
	}
	run.State, run.StateAt, run.ReadDue = "pending", at, at
	if run.Compensating {
		run.State = "compensating"
	}
	return c.DispatchTransport().QueueDispatch(c, receipt, *run, at)
}

func dispatchTerminal(state string) bool {
	return slices.Contains([]string{"completed", "cancelled", "compensated"}, state)
}

func (b *Build) updateDispatch(c platform.Caller, submission *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, submission, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		run, found := platform.Get[platform.DispatchRun](c, submission.GetTarget().GetId())
		if !found {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The dispatch does not exist")
		}
		var in struct {
			Outcome, Reason, Target, Intent string
			Revision                        uint32
			Answer                          json.RawMessage
			Reading                         *platform.DispatchReading
		}
		if json.Unmarshal(submission.GetPayload(), &in) != nil || len(in.Reason) > 4096 || len(in.Answer) > 64<<10 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid dispatch status")
		}
		manual := submission.GetSchema().GetName() == platform.DispatchReviewSchema
		transport := c.DispatchTransport()
		if transport == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Dispatch transport is unavailable")
		}
		if manual {
			if c.Agent || !c.Holds(ID, run.Definition.ReviewerRole) || strings.TrimSpace(in.Reason) == "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The dispatch needs its current reviewer and evidence")
			}
			typ, id, _ := strings.Cut(run.Subject, "/")
			if _, problem := c.ReadRecord(typ, id, at); problem != nil {
				return nil, problem
			}
			if !slices.Contains([]string{"retry", "retarget", "complete", "cancel"}, in.Outcome) || in.Reading != nil || in.Intent != "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid dispatch intervention")
			}
			if !slices.Contains([]string{"unknown", "stuck", "failed"}, run.State) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only an unresolved dispatch can be reviewed")
			}
			if in.Outcome == "retarget" && (in.Target == "" || in.Target == run.Target) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a different external target")
			}
		} else if !c.Automation {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Dispatch status belongs to its original transport")
		}
		if in.Reading != nil {
			if in.Revision != run.Revision || len(in.Reading.Observed) > 4096 || len(in.Reading.Reason) > 4096 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The dispatch changed while its status was read")
			}
			if problem := transport.CheckDispatchRead(run, at); problem != nil {
				return nil, problem
			}
		}
		if !manual && submission.GetSchema().GetName() != platform.DispatchTickSchema && !slices.Contains([]string{"accepted", "running", "completed", "stuck", "failed", "unknown"}, in.Outcome) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid dispatch result")
		}
		var replacementRequest, replacementConnection, replacementCompensation json.RawMessage
		if manual && in.Outcome == "retarget" {
			var problem *kernel.Error
			replacementRequest, replacementConnection, problem = transport.RetargetDispatch(c, run, in.Target, at)
			if problem != nil {
				return nil, problem
			}
			if len(run.CompensationRequest) > 0 {
				reverse := run
				reverse.Request = run.CompensationRequest
				replacementCompensation, _, problem = transport.RetargetDispatch(c, reverse, in.Target, at)
				if problem != nil {
					return nil, problem
				}
			}
		}
		return func(receipt *pb.ChangeRecord) {
			fail := func(problem *kernel.Error) bool {
				if problem != nil {
					c.FailDecision(problem)
					return true
				}
				return false
			}
			if manual {
				resolution := run
				resolution.State = "cancelled"
				if in.Outcome == "complete" {
					resolution.State = "completed"
				}
				if fail(transport.ResolveDispatchDelivery(c, resolution, at)) {
					return
				}
				run.Reason = in.Reason
				switch in.Outcome {
				case "complete":
					run.State = "completed"
					if run.Compensating {
						run.State = "compensated"
					}
				case "cancel":
					run.State = "cancelled"
				case "retarget", "retry":
					if in.Outcome == "retarget" {
						run.Target, run.Request, run.Connection = in.Target, replacementRequest, replacementConnection
						if len(replacementCompensation) > 0 {
							run.CompensationRequest = replacementCompensation
						}
					}
					run.Deadline = at.Add(time.Duration(run.Definition.DeadlineSeconds) * time.Second)
					run.PolicyApplied, run.Escalated = false, false
					if fail(b.queueDispatch(c, receipt, &run, at)) {
						return
					}
				}
				run.StateAt = at
			} else if !dispatchTerminal(run.State) {
				if in.Intent != "" {
					i := slices.IndexFunc(run.Attempts, func(attempt platform.DispatchAttempt) bool { return attempt.Intent == in.Intent })
					if i >= 0 {
						answer := in.Answer
						if len(answer) > 4096 {
							answer = platform.Raw(dataowner.ClipAnswer(answer))
						}
						run.Attempts[i].State, run.Attempts[i].Reason, run.Attempts[i].Answer = in.Outcome, in.Reason, answer
					}
				}
				if in.Reading != nil {
					in.Reading.At = at
					run.Readback = append(run.Readback, *in.Reading)
					if len(run.Readback) > 20 {
						run.Readback = run.Readback[len(run.Readback)-20:]
					}
					run.ReadDue = at.Add(time.Duration(run.Definition.Readback.EverySeconds) * time.Second)
				}
				if in.Outcome != "" && (in.Intent == "" || in.Intent == run.Intent) {
					if in.Reading != nil || !slices.Contains([]string{"running", "stuck", "failed"}, run.State) {
						if run.State != in.Outcome {
							run.StateAt = at
						}
						run.State, run.Reason = in.Outcome, in.Reason
						if in.Reading != nil && (run.State == "running" || run.State == "accepted") {
							run.PolicyApplied, run.Escalated = false, false
						}
						if run.Compensating && run.State == "completed" {
							run.State = "compensated"
						}
					}
				}
				if in.Reading != nil && in.Outcome != "unknown" {
					if fail(transport.ResolveDispatchDelivery(c, run, at)) {
						return
					}
				}
				if !at.Before(run.Deadline) && !dispatchTerminal(run.State) && run.State != "unknown" && run.State != "failed" {
					if run.State != "stuck" {
						run.StateAt = at
					}
					run.State, run.Reason = "stuck", "The dispatch reached its business deadline"
				}
			}
			if fail(b.dispatchState(c, receipt, &run, at)) {
				return
			}
			if fail(c.Put(receipt, run)) {
				return
			}
		}, nil
	})
}

func (b *Build) dispatchState(c platform.Caller, receipt *pb.ChangeRecord, run *platform.DispatchRun, at time.Time) *kernel.Error {
	if dispatchTerminal(run.State) {
		if err := c.DispatchTransport().ResolveDispatchDelivery(c, *run, at); err != nil {
			return err
		}
		c.Assign(receipt, platform.Assignment{Key: run.ID, Close: true})
		return nil
	}
	if !slices.Contains([]string{"stuck", "failed", "unknown"}, run.State) {
		c.Assign(receipt, platform.Assignment{Key: run.ID, Close: true})
		return nil
	}
	policy := "manual"
	if run.State == "stuck" {
		policy = run.Definition.OnStuck
	} else if run.State == "failed" {
		policy = run.Definition.OnFail
	}
	if !run.PolicyApplied && !run.Compensating && run.State != "unknown" {
		run.PolicyApplied = true
		switch policy {
		case "retry":
			if !run.Command.SafeRetry() {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This command cannot be automatically resent")
			}
			if at.Before(run.Deadline) {
				if err := c.DispatchTransport().ResolveDispatchDelivery(c, *run, at); err != nil {
					return err
				}
				return b.queueDispatch(c, receipt, run, at)
			}
		case "compensate":
			if run.CompensationCommand != nil && len(run.CompensationRequest) > 0 {
				if err := c.DispatchTransport().ResolveDispatchDelivery(c, *run, at); err != nil {
					return err
				}
				run.Compensating, run.Request = true, run.CompensationRequest
				run.Deadline = at.Add(time.Duration(run.Definition.DeadlineSeconds) * time.Second)
				return b.queueDispatch(c, receipt, run, at)
			}
		case "escalate":
			if !run.Escalated {
				run.Escalated = true
				c.Notify(platform.Notification{Key: "dispatch-attention:" + run.ID, Title: "Device dispatch needs attention", Body: run.Reason, Ref: platform.DispatchType + "/" + run.ID}, at, platform.Recipient{AppRole: run.Definition.ReviewerRole})
			}
		}
	}
	c.Assign(receipt, platform.Assignment{Key: run.ID, Ref: platform.DispatchType + "/" + run.ID, Title: run.Command.Title, Body: fmt.Sprintf("%s · %s\n%s", run.Command.Title, run.Subject, run.Reason), To: []platform.Recipient{{AppRole: run.Definition.ReviewerRole}}})
	return nil
}

func (b *Build) runDispatches(c platform.Caller, at time.Time) *kernel.Error {
	runs, err := platform.PendingDispatches(c)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if !at.Before(run.Deadline) && !slices.Contains([]string{"stuck", "failed", "unknown"}, run.State) {
			_, err := platform.Decide(c, b, dispatchSubmission(c, run.ID, platform.DispatchTickSchema, fmt.Sprintf("deadline:%d", run.Revision), map[string]any{}, at), at)
			return err
		}
	}
	return nil
}

func dispatchSubmission(c platform.Caller, id, schema, key string, payload any, at time.Time) *pb.Submission {
	return &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "dispatch:" + id + ":" + key, Target: &pb.EntityRef{Type: platform.DispatchType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
}
