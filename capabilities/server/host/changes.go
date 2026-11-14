package host

func (t *Tenant) changed() { t.changedOwner("") }

func (t *Tenant) operationsChanged(owner ...string) {
	name := ""
	if len(owner) > 0 {
		name = owner[0]
	}
	t.markChanged(false, name)
}

func (t *Tenant) changedOwner(owner string) {
	t.runIndex.Changed()
	t.markChanged(true, owner)
}

func (t *Tenant) markChanged(data bool, owner string) {
	if t.change.Mark(data, owner, func() bool { return t.readOrigin == nil && t.readImage.Load() != nil && !t.quarantined() }) {
		wakeWork()
	}
}

func (t *Tenant) Changes() (int64, <-chan struct{}) {
	seq, _, wake := t.change.State()
	return seq, wake
}
