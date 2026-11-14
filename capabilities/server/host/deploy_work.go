package host

import (
	"context"
	"fmt"
	"time"

	runtimekernel "platformserver/kernel"
	"platformserver/services/data/transform"
)

const roundSize = 10

func CodeOf(tenants ...*Tenant) string {
	var parts []string
	for _, t := range tenants {
		for _, a := range t.apps {
			parts = append(parts, fmt.Sprintf("%s %s@%s\n", t.ID, a.Manifest().ID, a.Manifest().Version))
		}
	}
	return runtimekernel.CodeDigest(parts...)
}

var workReady = runtimekernel.NewWake()

func wakeWork() { workReady.Signal() }

var activeTicks = []func(*Tenant, time.Time){(*Tenant).Evaluate, (*Tenant).PullSources, (*Tenant).CheckConnections, (*Tenant).RunPipelines,
	(*Tenant).ProjectDatasets, (*Tenant).Embed, (*Tenant).PurgeTranscripts, (*Tenant).SweepUploads}

func runWorkFrom(current func() []*Tenant) func(context.Context) error {
	return runtimekernel.WorkLoop[*Tenant]{
		Tenants: current, Now: Now, Wake: workReady,
		Round: func(t *Tenant, now time.Time) bool { return t.Round(now, roundSize) },
		Outside: func(t *Tenant) []func() {
			outside := append(t.dispatches(Now()), t.agentSchedule().Turns().Due(Now())...)
			return append(outside, t.continuousSources(Now())...)
		},
		OnLane: integrations.OnLane,
		Always: func(t *Tenant) {
			t.FlushObservations()
			t.PurgeObservations(Now())
		},
		Stopped: (*Tenant).businessStopped,
		Active: func(t *Tenant) {
			for _, tick := range activeTicks {
				tick(t, Now())
			}
		},
	}.Start()
}
