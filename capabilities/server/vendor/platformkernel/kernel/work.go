package kernel

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// Works implements K9 (contract/spec/K9-work-ownership.md).
type Works struct{ items map[string]*pb.Work }

func NewWorks() *Works { return &Works{items: map[string]*pb.Work{}} }

func (w *Works) running(id string, generation uint32) (*pb.Work, *Error) {
	work := w.items[id]
	if work == nil {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND) // W6
	}
	if work.GetState() != pb.WorkState_WORK_STATE_RUNNING || work.GetGeneration() != generation {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_CONFLICT) // W2: stale or finished
	}
	return work, nil
}

// Start begins the next generation and returns the checkpoint to resume from (W1).
func (w *Works) Start(id, owner string) (uint32, string, *Error) {
	if id == "" || owner == "" {
		return 0, "", errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	work := w.items[id]
	if work == nil {
		work = &pb.Work{WorkId: id}
		w.items[id] = work
	} else if work.GetState() == pb.WorkState_WORK_STATE_RUNNING {
		return 0, "", errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	}
	work.OwnerId, work.Generation, work.State, work.Progress = owner, work.GetGeneration()+1, pb.WorkState_WORK_STATE_RUNNING, 0
	return work.GetGeneration(), work.GetCheckpoint(), nil
}

// Checkpoint records progress; an empty checkpoint keeps the previous one (W2, W3).
func (w *Works) Checkpoint(id string, generation, progress uint32, checkpoint string) *Error {
	work, err := w.running(id, generation)
	if err != nil {
		return err
	}
	if progress < work.GetProgress() {
		return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	}
	work.Progress = progress
	if checkpoint != "" {
		work.Checkpoint = checkpoint
	}
	return nil
}

// Finish completes or fails the current generation (W2).
func (w *Works) Finish(id string, generation uint32, failed bool) *Error {
	work, err := w.running(id, generation)
	if err != nil {
		return err
	}
	work.State = pb.WorkState_WORK_STATE_COMPLETED
	if failed {
		work.State = pb.WorkState_WORK_STATE_FAILED
	}
	return nil
}

func (w *Works) Cancel(id string) *Error {
	work, err := w.running(id, w.items[id].GetGeneration())
	if err != nil {
		return err // W4, W6
	}
	work.State = pb.WorkState_WORK_STATE_CANCELLED
	return nil
}

// CloseOwner cancels the owner's running work; finished work stays (W5).
func (w *Works) CloseOwner(owner string) {
	for _, work := range w.items {
		if work.GetOwnerId() == owner && work.GetState() == pb.WorkState_WORK_STATE_RUNNING {
			work.State = pb.WorkState_WORK_STATE_CANCELLED
		}
	}
}

func (w *Works) Get(id string) (*pb.Work, *Error) {
	if work := w.items[id]; work != nil {
		return work, nil
	}
	return nil, errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
}
