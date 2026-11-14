package automation

func (b *WorkBoard) ForkInto(tenant string, view *WorkBoard, known func(app string) bool) error {
	b.Lock()
	work, err := b.Snapshot()
	for _, job := range b.Jobs {
		if current := view.Job(job.ID); current != nil {
			current.NextRun = job.NextRun
		}
	}
	b.Unlock()
	if err != nil {
		return err
	}
	return view.Install(tenant, WorkState{Queues: work.Queues, Failed: work.Failed, Tasks: work.Tasks, Jobs: work.Jobs}, known)
}

func (b *WorkBoard) CopyRotation(from *WorkBoard) {
	from.Lock()
	b.Rotation = from.Rotation
	from.Unlock()
}
