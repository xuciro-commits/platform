package construction

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

// ProcessExecution supplies the original authorized read callbacks captured by
// a compiled flow. CompileProcess only constructs the native definition; it
// does not call these functions or own an execution/scheduling path.
type ProcessExecution struct {
	StateField        string
	ContinuousEnabled func(platform.Caller) bool
	Subject           func(platform.Caller, *platform.Run) (json.RawMessage, *kernel.Error)
	SubjectPath       func(platform.Caller, *platform.Run, []string) (json.RawMessage, []string, *kernel.Error)
	Query             func(platform.Caller, *platform.Run, string, string, int, json.RawMessage) (json.RawMessage, *kernel.Error)
}

// CompileProcess lowers every declarative block to the existing native owner.
func CompileProcess(p Process, execution ProcessExecution) platform.Flow {
	fl := platform.Flow{Name: p.Name, Title: p.Title, Version: p.Version, Subject: p.Object, Owners: []string{Builder}, Start: platform.Start{Manual: p.Manual}, Continuous: p.Continuous}
	if p.Every != "" {
		fl.Start.Every, _ = time.ParseDuration(p.Every)
		fl.Start.OnBehalf = p.Scheduler
	} else if p.Continuous != nil && p.Continuous.Intake != nil && !p.Manual {
		// Retained manual versions keep their original start semantics. New
		// continuous publishes reject Manual and record their publishing member.
		fl.Start.Manual, fl.Start.Continuous, fl.Start.OnBehalf = false, true, p.Scheduler
		fl.Start.Enabled = execution.ContinuousEnabled
	} else if !p.Manual {
		fl.Start.Type = p.Object
		stateField := execution.StateField
		fl.Start.When = func(_ platform.Caller, record any) bool {
			raw, _ := json.Marshal(record)
			var value map[string]json.RawMessage
			json.Unmarshal(raw, &value)
			var state string
			json.Unmarshal(value[stateField], &state)
			return state == p.When
		}
	}
	for _, node := range p.Steps {
		s := node
		step := platform.Step{Name: s.Name, Title: cmp.Or(s.Title, s.Name), Next: s.Next, Fault: s.Error, Timeout: time.Duration(s.TimeoutSeconds) * time.Second, OnTimeout: s.Error}
		if s.RetryAttempts > 0 || s.RetryBackoffMs > 0 {
			step.Retry = &platform.RetryPolicy{Attempts: s.RetryAttempts, Backoff: time.Duration(s.RetryBackoffMs) * time.Millisecond}
		}
		subject := func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
			if p.Object == "" {
				return json.RawMessage("{}"), nil
			}
			return execution.Subject(c, r)
		}
		resolve := func(c platform.Caller, r *platform.Run, binding platform.Binding) (json.RawMessage, *kernel.Error) {
			// Identity belongs to the already authorized record, not its
			// declared business fields. Other paths retain field checks.
			if binding.Source == "subject" && len(binding.Path) > 0 && binding.Path[0] != "id" {
				value, sources, err := execution.SubjectPath(c, r, binding.Path)
				r.Sources = append(r.Sources, sources...)
				return value, err
			}
			var raw json.RawMessage
			if binding.Source == "subject" {
				var err *kernel.Error
				raw, err = subject(c, r)
				if err != nil {
					return nil, err
				}
			}
			value, err := binding.Resolve(r, raw)
			if err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return value, nil
		}
		inputs := func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
			values := map[string]json.RawMessage{}
			for _, name := range slices.Sorted(maps.Keys(s.Inputs)) {
				value, err := resolve(c, r, s.Inputs[name])
				if err != nil {
					return nil, err
				}
				values[name] = value
			}
			return platform.Raw(values), nil
		}
		predicate := func(c platform.Caller, r *platform.Run) (bool, *kernel.Error) {
			raw := json.RawMessage("{}")
			if predicateSubject(*s.Condition) {
				var err *kernel.Error
				raw, err = subject(c, r)
				if err != nil {
					return false, err
				}
			}
			holds, problem := s.Condition.Test(r, raw)
			if problem != nil {
				return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, problem.Error())
			}
			return holds, nil
		}
		target := func(c platform.Caller, r *platform.Run) string {
			if s.Target == nil {
				return r.Key
			}
			raw, err := resolve(c, r, *s.Target)
			if err != nil {
				return ""
			}
			var value string
			json.Unmarshal(raw, &value)
			return value
		}
		switch s.Kind {
		case "payload", "transform":
			step.Evaluate = &platform.Evaluate{Run: func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
				if s.Value != nil {
					return resolve(c, r, *s.Value)
				}
				if s.Kind == "payload" && len(s.Inputs) == 0 {
					return r.Data, nil
				}
				return inputs(c, r)
			}}
		case "query":
			step.Evaluate = &platform.Evaluate{Run: func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
				raw, err := inputs(c, r)
				if err != nil {
					return nil, err
				}
				answer, err := execution.Query(c, r, s.App, s.Query, s.QueryVersion, raw)
				if err == nil {
					var provenance struct {
						Sources []string `json:"sources"`
					}
					json.Unmarshal(answer, &provenance)
					r.Sources = append(r.Sources, provenance.Sources...)
				}
				return answer, err
			}}
		case "branch":
			step.Branch = &platform.Branch{Paths: []string{s.Cases["true"], s.Cases["false"]}, Choose: func(c platform.Caller, r *platform.Run) (string, string, *kernel.Error) {
				holds, err := predicate(c, r)
				name := fmt.Sprint(holds)
				return s.Cases[name], name, err
			}}
		case "switch":
			step.Branch = &platform.Branch{Paths: append([]string{s.Next}, slices.Collect(maps.Values(s.Cases))...), Choose: func(c platform.Caller, r *platform.Run) (string, string, *kernel.Error) {
				raw, err := resolve(c, r, *s.Value)
				if err != nil {
					return "", "", err
				}
				var value string
				if json.Unmarshal(raw, &value) != nil {
					value = string(raw)
				}
				return cmp.Or(s.Cases[value], s.Next), value, nil
			}}
		case "foreach", "while":
			step.Loop = &platform.Loop{Body: s.Body, MaxIterations: s.MaxIterations, Concurrency: max(1, s.Concurrency)}
			if s.Kind == "while" {
				step.Loop.While = predicate
				step.Loop.Initial = func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
					if s.Value != nil {
						return resolve(c, r, *s.Value)
					}
					return inputs(c, r)
				}
			} else {
				step.Loop.Items = func(c platform.Caller, r *platform.Run) ([]json.RawMessage, *kernel.Error) {
					raw, err := resolve(c, r, *s.Collection)
					if err != nil {
						return nil, err
					}
					var items []json.RawMessage
					if json.Unmarshal(raw, &items) != nil {
						return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "ForEach needs an array")
					}
					return items, nil
				}
			}
		case "fork":
			if s.Mode == "any" {
				step.Any = s.Branches
			} else {
				step.All = s.Branches
			}
		case "break", "continue":
			step.LoopControl = s.Kind
			if s.Value != nil {
				step.Output = func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
					return resolve(c, r, *s.Value)
				}
			}
		case "join", "end":
			step.End = true
			if s.Value != nil {
				step.Output = func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
					return resolve(c, r, *s.Value)
				}
			}
		case "fail":
			step.NoRetry = true
			step.Evaluate = &platform.Evaluate{Run: func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, cmp.Or(s.Title, "Flow failed"))
			}}
		case "action":
			schema := s.Act
			if !strings.Contains(schema, ".") {
				schema = p.Object + "." + schema
			}
			step.Act = &platform.Act{Action: schema, Protocol: s.Protocol, AsMember: s.Protocol == "", Target: target, TargetValue: func(c platform.Caller, r *platform.Run) (string, *kernel.Error) {
				if s.Target == nil {
					return r.Key, nil
				}
				raw, err := resolve(c, r, *s.Target)
				if err != nil {
					return "", err
				}
				var value string
				if json.Unmarshal(raw, &value) != nil || value == "" {
					return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Action target must be a record ID")
				}
				return value, nil
			}, Inputs: inputs, Payload: func(c platform.Caller, r *platform.Run) any {
				raw, err := inputs(c, r)
				if err != nil {
					return json.RawMessage("null")
				}
				return raw
			}}
		case "ask":
			step.Ask = &platform.Ask{Answers: s.Answers, Title: func(_ platform.Caller, r *platform.Run) string { return cmp.Or(s.Title, s.Name) + ": " + r.Key }, Ref: func(_ platform.Caller, r *platform.Run) string {
				return cmp.Or(p.Object, "flow.instance") + "/" + r.Key
			}, To: func(platform.Caller, *platform.Run) []platform.Recipient {
				return []platform.Recipient{{AppRole: s.Ask}}
			}}
			step.Output = func(_ platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
				return platform.Raw(map[string]string{"answer": r.Answer}), nil
			}
			if len(s.Cases) > 0 {
				step.Choose = func(_ platform.Caller, r *platform.Run) (string, string) {
					return cmp.Or(s.Cases[r.Answer], s.Next), r.Answer
				}
			}
		case "wait":
			step.Wait = &platform.Wait{}
			if s.Condition != nil {
				step.Wait.Until = func(c platform.Caller, r *platform.Run) bool {
					holds, err := predicate(c, r)
					return err == nil && holds
				}
			} else {
				step.Wait.At = func(_ platform.Caller, r *platform.Run) time.Time {
					return r.Now.Add(time.Duration(s.UntilSeconds) * time.Second)
				}
			}
		case "subflow":
			step.Call = &platform.Call{Flow: s.Flow, Version: s.FlowVersion, Data: func(c platform.Caller, r *platform.Run) any {
				raw, err := inputs(c, r)
				if err != nil {
					return json.RawMessage("null")
				}
				return raw
			}}
		case "compute":
			step.Operation = &platform.OperationStep{Request: func(c platform.Caller, r *platform.Run) (platform.OperationRequest, *kernel.Error) {
				raw, err := inputs(c, r)
				if s.Value != nil {
					raw, err = resolve(c, r, *s.Value)
				}
				if err != nil {
					return platform.OperationRequest{}, err
				}
				return platform.OperationRequest{App: cmp.Or(s.Operation.App, ID), Name: s.Operation.Name, Version: s.Operation.Version, Inputs: raw}, nil
			}}
		case "ai":
			step.Invoke = &platform.Invocation{Act: platform.Act{Action: SchemaFunctionCall, Target: func(_ platform.Caller, r *platform.Run) string {
				return fmt.Sprintf("%s:%s:%d", r.ID, s.Name, r.Sequence)
			}, Payload: func(c platform.Caller, r *platform.Run) any {
				return platform.FunctionRequest{App: s.Function.App, Name: s.Function.Name, Version: s.Function.Version, Source: target(c, r), OnBehalf: r.OnBehalf, Release: &r.Release}
			}}, Result: func(c platform.Caller, r *platform.Run, id string) (json.RawMessage, bool, *kernel.Error) {
				call, ok := platform.Get[FunctionRun](c, id)
				if !ok || call.State == "pending" {
					return nil, false, nil
				}
				if call.State != "ready" {
					return nil, true, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, cmp.Or(call.Reason, "AI function rejected its answer"))
				}
				r.Sources = append(r.Sources, call.Sources...)
				return json.RawMessage(call.Output), true, nil
			}}
		}
		fl.Steps = append(fl.Steps, step)
	}
	return fl
}

func predicateSubject(p platform.Predicate) bool {
	if p.Left != nil && p.Left.Source == "subject" || p.Right != nil && p.Right.Source == "subject" {
		return true
	}
	for _, term := range p.Terms {
		if predicateSubject(term) {
			return true
		}
	}
	return false
}
