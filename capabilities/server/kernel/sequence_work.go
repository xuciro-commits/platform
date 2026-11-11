package kernel

import (
	"encoding/json"
	"fmt"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func ForkWorks(works *kernel.Works) *kernel.Works {
	items := works.All()
	for i, item := range items {
		items[i] = proto.Clone(item).(*pb.Work)
	}
	draft := kernel.NewWorks()
	draft.Restore(items)
	return draft
}

func InstallWork(works *kernel.Works, finished *pb.Work) {
	all := works.All()
	found := false
	for i, work := range all {
		if work.GetWorkId() == finished.GetWorkId() {
			all[i], found = finished, true
			break
		}
	}
	if !found {
		all = append(all, finished)
	}
	works.Restore(all)
}

type WorkApply struct {
	Changes   ChangeApply
	Works     *kernel.Works
	App       func(string) platform.App
	Task      func(acceptedformat.Work) (platform.ScheduledTaskState, string, func(*pb.Work), error)
	Published func(*PreparedChanges)
}

func (s *Sequence) ApplyWorkResult(raw []byte, h WorkApply) (bool, error) {
	result, err := acceptedformat.DecodeWork(raw)
	if err != nil || result.Tenant != s.ID || h.App(result.App) == nil {
		return false, fmt.Errorf("work result cannot be applied: %v", err)
	}
	finished := new(pb.Work)
	_ = protojson.Unmarshal(result.Work, finished)
	previous, previousErr := h.Works.Get(result.Input.ID)
	if previousErr == nil && previous.GetGeneration() >= finished.GetGeneration() {
		if previous.GetGeneration() == finished.GetGeneration() && !proto.Equal(previous, finished) {
			return false, fmt.Errorf("work result generation differs")
		}
		return false, nil
	}
	before, taskID, settle, err := h.Task(result)
	if err != nil {
		return false, err
	}
	var prior json.RawMessage
	if previousErr == nil {
		prior, _ = protojson.Marshal(previous)
	}
	hash, hashErr := acceptedformat.WorkInputHash(before, prior)
	if hashErr != nil || hash != result.RequestHash {
		return false, fmt.Errorf("work result predecessor differs")
	}
	works := ForkWorks(h.Works)
	generation, _, refusal := works.Start(taskID, "host")
	if refusal != nil || int(generation) != result.After.Attempts ||
		works.Finish(taskID, generation, result.Outcome != "ok") != nil {
		return false, fmt.Errorf("work result K9 generation differs")
	}
	expected, _ := works.Get(taskID)
	if !proto.Equal(expected, finished) {
		return false, fmt.Errorf("work result K9 state differs")
	}
	prepared, err := s.PrepareChanges(result.ChangePlan(), h.Changes)
	if err != nil {
		return false, err
	}
	if err := s.PromoteChanges(prepared, h.Changes); err != nil {
		return false, err
	}
	InstallWork(h.Works, finished)
	settle(finished)
	h.Published(prepared)
	return true, nil
}
