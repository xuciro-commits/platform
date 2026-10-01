package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Bind page inputs from the same scoped record read used by Flow. Source
// labels are supplied by the host, never stripped by a compute guest.
func (t *Tenant) bindCapabilityInputs(m platform.Member, q platform.CapabilityInvocation, now time.Time) (json.RawMessage, []string, *kernel.Error) {
	refuse := func(message string) (json.RawMessage, []string, *kernel.Error) {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message)
	}
	if len(q.Bindings) > 64 {
		return refuse("Input bindings exceed their bound")
	}
	sources := slices.Clone(q.Sources)
	if len(q.Bindings) == 0 {
		if q.Record != "" {
			return refuse("A source record needs a subject input binding")
		}
		return q.Inputs, sources, nil
	}
	caller := platform.NewCaller(runtime{t}, m, q.Ref.App, false, false)
	var subject json.RawMessage
	usedRecord := false
	values := map[string]json.RawMessage{}
	for name, binding := range q.Bindings {
		if binding.Source != "literal" && binding.Source != "input" && binding.Source != "subject" {
			return refuse("Page inputs use constants, form input or the selected record")
		}
		if binding.Source == "subject" {
			if q.Record == "" {
				return refuse("Select a readable source record")
			}
			if len(binding.Path) > 0 {
				typ, id, ok := strings.Cut(q.Record, "/")
				if !ok || id == "" || binding.Check() != nil {
					return refuse("Source needs an object, record ID and bounded path")
				}
				value, refs, problem := caller.ReadRecordPath(typ, id, binding.Path, now)
				if problem != nil {
					return nil, nil, problem
				}
				values[name] = value
				sources = append(sources, refs...)
				usedRecord = true
				continue
			}
			if subject == nil {
				typ, id, ok := strings.Cut(q.Record, "/")
				if !ok || id == "" {
					return refuse("Source needs an object and record ID")
				}
				var err *kernel.Error
				subject, err = caller.ReadRecord(typ, id, now)
				if err != nil {
					return nil, nil, err
				}
			}
			usedRecord = true
			sources = append(sources, q.Record)
			if len(binding.Path) > 0 {
				sources = append(sources, q.Record+"#"+binding.Path[0])
			} else {
				var fields map[string]json.RawMessage
				_ = json.Unmarshal(subject, &fields)
				for field := range fields {
					if field != "id" && field != "revision" && field != "created" && field != "changed" && field != "archived" {
						sources = append(sources, q.Record+"#"+field)
					}
				}
			}
		}
		value, err := binding.Resolve(&platform.Run{Data: q.Inputs}, subject)
		if err != nil {
			return refuse(err.Error())
		}
		values[name] = value
	}
	if q.Record != "" && !usedRecord {
		return refuse("A source record needs a subject input binding")
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return refuse(err.Error())
	}
	slices.Sort(sources)
	return raw, slices.Compact(sources), nil
}
