package platformserver

// States and actions a tenant gives its own object: lost
// property found at a desk, handed back to whoever claims it, with the rules
// the builder wrote checked in the decision and again in replay.
// merge is a with b's keys laid over it.
func merge(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
