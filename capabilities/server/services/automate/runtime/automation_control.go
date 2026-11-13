package automation

import (
	"encoding/json"

	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type ControlWork struct {
	Queues map[string][]string           `json:"queues"`
	Failed []string                      `json:"failed"`
	Tasks  []platform.ScheduledTaskState `json:"tasks"`
	Jobs   []Task                        `json:"jobs"`
}

func (b *WorkBoard) ControlImage() (ControlWork, error) {
	work, err := b.Snapshot()
	return ControlWork{work.Queues, work.Failed, work.Tasks, work.Jobs}, err
}

func (b *WorkBoard) InstallImage(tenant string, raw json.RawMessage, known func(app string) bool) error {
	var w ControlWork
	if err := acceptedformat.DecodeControl(raw, &w); err != nil {
		return err
	}
	return b.Install(tenant, WorkState{Queues: w.Queues, Failed: w.Failed, Tasks: w.Tasks, Jobs: w.Jobs}, known)
}

func (b *WorkBoard) CopyJobs(to *WorkBoard) {
	b.Lock()
	defer b.Unlock()
	for _, j := range b.Jobs {
		copy := *j
		to.AddJob(&copy)
	}
}
