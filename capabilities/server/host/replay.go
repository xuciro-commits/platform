package host

import (
	runtimekernel "platformserver/kernel"
	controlowner "platformserver/services/security/control"

	"platformserver/platform"
	"platformserver/services/actions"
	"time"
)

func (t *Tenant) Replay(entries []Entry) error {
	platformMember := "app:" + controlowner.PlatformApp
	return t.ReplayEntries(entries, runtimekernel.ReplayHooks{
		Rules: map[string]runtimekernel.ReplayRule{
			"release-result": {Label: "immutable release result", Apply: func(body []byte) (runtimekernel.ReplayedResult, error) {
				saved, err := t.releaseAcceptance().Apply(body)
				return runtimekernel.ReplayedResult{App: saved.App, Member: saved.Member, At: saved.At}, err
			}},
			"composite-result": {Label: "composite result", Apply: func(body []byte) (runtimekernel.ReplayedResult, error) {
				saved, err := t.applyAcceptedComposite(body)
				return runtimekernel.ReplayedResult{App: saved.App, Member: saved.Member, At: saved.At, After: func(time.Time) { t.changed() }}, err
			}},
			"input-result": {Label: "connector result", Applied: true, Apply: func(body []byte) (runtimekernel.ReplayedResult, error) {
				saved, applied, err := t.applyAcceptedInput(body)
				return runtimekernel.ReplayedResult{App: saved.App, Member: saved.Member, At: saved.At, Applied: applied, After: func(at time.Time) {
					if saved.Refusal == nil {
						t.AuditLog.Remember(platform.AuditEntry{At: at, Member: saved.Member, App: saved.App, Action: "input:" + saved.Name})
						t.enqueue(at)
					}
				}}, err
			}},
			"effect-result": {Label: "effect result", Applied: true, Apply: func(body []byte) (runtimekernel.ReplayedResult, error) {
				saved, applied, err := t.applyAcceptedEffect(body)
				return runtimekernel.ReplayedResult{App: saved.App, Member: platformMember, At: saved.At, Applied: applied, After: func(time.Time) { t.enqueue(saved.At) }}, err
			}},
			"usage-result": {Label: "model usage result", Apply: func(body []byte) (runtimekernel.ReplayedResult, error) {
				saved, err := t.ApplyUsageResult(body, t.usage())
				return runtimekernel.ReplayedResult{App: saved.App, Member: saved.Member, At: saved.At}, err
			}},
			"agent-result": {Label: "agent result", Apply: func(body []byte) (runtimekernel.ReplayedResult, error) {
				saved, err := t.applyAcceptedAgent(body)
				return runtimekernel.ReplayedResult{App: saved.App, Member: saved.Member, At: saved.At, After: func(time.Time) { t.enqueue(saved.At) }}, err
			}},
			"operation-claim": {Label: "operation claim", Apply: func(body []byte) (runtimekernel.ReplayedResult, error) {
				saved, err := t.operationClaims().Apply(body)
				return runtimekernel.ReplayedResult{App: saved.App, Member: platformMember, At: saved.At, After: func(time.Time) {
					t.outbound.Update(saved.Before.ID, func(x *actions.Queued) { x.Sending = false })
				}}, err
			}},
		},
		HostControl: t.hostControl,
		Work: func(body []byte) error {
			_, err := t.applyAcceptedWork(body)
			return err
		},
		Batch:      t.applyAcceptedBatch,
		Record:     t.applyAcceptedResult,
		Invalidate: t.invalidateReadImage,
		Pin: func(versions map[string]int) {
			if t.procs != nil {
				t.procs.Pin(versions)
			}
		},
		App:           t.App,
		Enqueue:       t.enqueue,
		PublishRecord: t.publishAccepted,
	})
}
