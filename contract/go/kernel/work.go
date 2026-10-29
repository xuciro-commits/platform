package kernel

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

type Works struct{ items map[string]*pb.Work }

func NewWorks() *Works { return &Works{items: map[string]*pb.Work{}} }

func (w *Works) running(id string, generation uint32) (*pb.Work, *Error) {
	work := w.items[id]
	if work == nil {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	if work.GetState() != pb.WorkState_WORK_STATE_RUNNING || work.GetGeneration() != generation {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	}
	return work, nil
}

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
		return err
	}
	work.State = pb.WorkState_WORK_STATE_CANCELLED
	return nil
}

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
