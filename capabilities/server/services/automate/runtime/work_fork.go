package runtime

func (b *WorkBoard) ForkInto(view *WorkBoard) {
	b.Lock()
	defer b.Unlock()
	clone := func(tasks []*Task) []*Task {
		out := make([]*Task, len(tasks))
		for i, task := range tasks {
			copy := *task
			out[i] = &copy
		}
		return out
	}
	for app, queue := range b.Queues {
		view.Queues[app] = clone(queue)
	}
	view.Failed = clone(b.Failed)
	view.Jobs = clone(b.Jobs)
	view.Rotation = b.Rotation
}
