package flow

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// validateArtifactCleanup protects live frame/checkpoint references from a
// stale or malformed cleanup intent. A successful successor can retire at
// most its former sealed frame and, on checkpoint rollover, its former
// checkpoint.
func validateArtifactCleanup(x FlowInstance) error {
	if x.CleanupPending != (len(x.ArtifactCleanup) > 0) || len(x.ArtifactCleanup) > 2 {
		return fmt.Errorf("the Flow artifact cleanup marker is inconsistent")
	}
	seen := map[string]bool{}
	tenant := ""
	if x.Batch != nil && x.Batch.Sealed != nil {
		tenant = x.Batch.Sealed.Tenant
	}
	for _, ref := range x.ArtifactCleanup {
		if ref.Tenant == "" || ref.Instance != x.ID || ref.Version != x.Version || ref.Ticket == "" || ref.Digest == "" || ref.Size < 1 {
			return fmt.Errorf("a retired Flow artifact has an invalid owner or bound")
		}
		if tenant == "" {
			tenant = ref.Tenant
		} else if tenant != ref.Tenant {
			return fmt.Errorf("retired Flow artifacts belong to different tenants")
		}
		if x.Batch != nil {
			if x.Batch.Sealed != nil && sameArtifact(*x.Batch.Sealed, ref) || x.Batch.CheckpointArtifact != nil && sameArtifact(*x.Batch.CheckpointArtifact, ref) {
				return fmt.Errorf("a live Flow artifact cannot be retired")
			}
		}
		key := string(platform.Raw(ref))
		if seen[key] {
			return fmt.Errorf("a Flow artifact is listed for retirement more than once")
		}
		seen[key] = true
	}
	return nil
}

func sameArtifact(a, b platform.FlowStateArtifact) bool {
	return bytes.Equal(platform.Raw(a), platform.Raw(b))
}

func cloneArtifacts(refs []platform.FlowStateArtifact) []platform.FlowStateArtifact {
	return slices.Clone(refs)
}

// ArtifactCleanupPreparation snapshots the original Flow owner before deleting
// bytes outside the tenant lock. Its acknowledgement then uses the same accepted
// Flow ledger/result path as every other durable instance change.
type ArtifactCleanupPreparation struct {
	owner  *Flows
	before FlowInstance
	refs   []platform.FlowStateArtifact
	digest string
	at     time.Time
	tenant string
}

func (f *Flows) PlanArtifactCleanup(x FlowInstance, at time.Time) (*ArtifactCleanupPreparation, *kernel.Error) {
	if !x.CleanupPending || len(x.ArtifactCleanup) == 0 || at.IsZero() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow has no due artifact cleanup")
	}
	if err := validateArtifactCleanup(x); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	refs := cloneArtifacts(x.ArtifactCleanup)
	raw := platform.Raw([]any{x.ID, x.Version, refs})
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	return &ArtifactCleanupPreparation{owner: f, before: x, refs: refs, digest: digest, at: at, tenant: refs[0].Tenant}, nil
}

func (p *ArtifactCleanupPreparation) Instance() string {
	if p == nil {
		return ""
	}
	return p.before.ID
}

func (p *ArtifactCleanupPreparation) Artifacts() []platform.FlowStateArtifact {
	return cloneArtifacts(p.refs)
}

func (p *ArtifactCleanupPreparation) Matches(x FlowInstance) bool {
	return p != nil && x.ID == p.before.ID && x.Revision == p.before.Revision && x.Flow == p.before.Flow && x.Version == p.before.Version &&
		x.CleanupPending && bytes.Equal(platform.Raw(x.ArtifactCleanup), platform.Raw(p.refs))
}

func (p *ArtifactCleanupPreparation) Submission(tenant string) *pb.Submission {
	return &pb.Submission{TenantId: tenant, Authority: ID, PrincipalId: "app:" + ID,
		IdempotencyKey: fmt.Sprintf("flow-artifact-cleanup:%x", sha256.Sum256(platform.Raw([]any{p.before.ID, p.before.Version, p.refs}))),
		Target:         &pb.EntityRef{Type: InstanceType, Id: p.before.ID}, Schema: &pb.SchemaRef{Name: SchemaFlowArtifactCleanup, Version: 1},
		Payload: platform.Raw(map[string]string{"digest": p.digest})}
}

func (p *ArtifactCleanupPreparation) Decision() platform.ResultApp {
	return preparedArtifactCleanupDecision{Flows: p.owner, prepared: p}
}

type preparedArtifactCleanupDecision struct {
	*Flows
	prepared *ArtifactCleanupPreparation
}

func (d preparedArtifactCleanupDecision) Submit(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	p := d.prepared
	if p == nil || !c.Automation || c.ID != "app:"+ID || c.Tenant != p.tenant || sub.GetTenantId() != p.tenant || sub.GetAuthority() != ID || sub.GetPrincipalId() != c.ID ||
		sub.GetTarget().GetType() != InstanceType || sub.GetTarget().GetId() != p.before.ID || sub.GetSchema().GetName() != SchemaFlowArtifactCleanup || sub.GetSchema().GetVersion() != 1 ||
		!bytes.Equal(sub.GetPayload(), p.Submission(p.tenant).GetPayload()) || sub.GetIdempotencyKey() != p.Submission(p.tenant).GetIdempotencyKey() || !now.Equal(p.at) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the host can acknowledge retired Flow artifacts")
	}
	return d.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		ss := d.session(c, now)
		x := ss.load(p.before.ID)
		if x == nil || !p.Matches(*x) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its artifacts were being retired")
		}
		x.ArtifactCleanup, x.CleanupPending = nil, false
		ss.trace(x, "", "artifact cleanup", fmt.Sprintf("retired %d superseded frame/checkpoint artifacts", len(p.refs)), "host")
		return ss.apply, nil
	})
}
