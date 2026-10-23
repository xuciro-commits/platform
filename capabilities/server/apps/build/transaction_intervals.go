package build

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

func intervalFields(f platform.TxnIntervalFields) map[string]string {
	return map[string]string{f.Subject: "reference", f.Source: "text", f.SourceVersion: "text", f.SourceRevision: "text", f.ProcessingVersion: "text", f.Since: "datetime", f.Until: "datetime", f.OpenStart: "boolean", f.OpenEnd: "boolean", f.Retired: "boolean", f.Review: "boolean"}
}

func (b *Build) checkTxnIntervals(step platform.TxnStep, scope platform.RuleReadSpec, lookup func(string) (platform.EntityInfo, bool)) error {
	p := step.Intervals
	if p == nil || (platform.AssetRef{App: construction.ID, Kind: platform.AssetObservation, Name: p.Source}).Check() != nil || !construction.Named(p.Set) || step.Target != (platform.TxnRef{}) {
		return fmt.Errorf("Interval projection needs its logical source and original collection")
	}
	subject, found := lookup(scope.Object)
	if !found {
		return fmt.Errorf("Interval projection subject is unavailable")
	}
	var collection *platform.RecordSet
	for i := range subject.Sets {
		if subject.Sets[i].Name == p.Set {
			collection = &subject.Sets[i]
			break
		}
	}
	if collection == nil || len(collection.Order) == 0 || collection.Object == scope.Object {
		return fmt.Errorf("Interval projection needs a complete ordered collection of another owned object")
	}
	output, found := lookup(collection.Object)
	if !found || output.App != construction.ID {
		return fmt.Errorf("Interval projection output must belong to its authority")
	}
	fields := intervalFields(p.Fields)
	if len(fields) != 11 {
		return fmt.Errorf("Interval projection fields must be distinct")
	}
	for name, kind := range fields {
		field, found := output.Field(name)
		if name == "" || !found || field.ReadOnly || field.Type != kind || kind == "reference" && field.Ref != scope.Object {
			return fmt.Errorf("Interval projection field %q needs a writable %s of its subject", name, kind)
		}
	}
	link, err := b.transactionLink(p.Predecessors)
	if err != nil {
		return err
	}
	if link.Storage != "edge" || link.Parent.Name != output.Type || link.Child.Name != output.Type || link.Cardinality != "many-to-many" {
		return fmt.Errorf("Interval predecessors need the output object's owned many-to-many relation")
	}
	return nil
}

func (b *Build) txnIntervals(c platform.Caller, receipt *pb.ChangeRecord, subject, subjectID, step string, p platform.TxnIntervals, iterations *int, now time.Time, reserve func(string) *kernel.Error) *kernel.Error {
	proof := c.ObservationContext()
	if proof == nil || proof.Intervals == nil || proof.Record != subject+"/"+subjectID || proof.Definition.Ref != (platform.AssetRef{App: construction.ID, Kind: platform.AssetObservation, Name: p.Source}) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Interval projection requires its accepted observation / 区间写入须有对应观测依据")
	}
	set, info, ids, failure := b.txnRecordRows(c, receipt, subject, subjectID, p.Set, step, iterations, now)
	if failure != nil {
		return failure
	}
	entity, installed := b.installed[info.Type]
	if !installed {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Interval output object is unavailable")
	}
	for name := range intervalFields(p.Fields) {
		field, found := info.Field(name)
		if !found || !field.ReadsAny(c.RolesIn(info.App)) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Interval projection contains withheld fields / 区间集合含不可见字段")
		}
	}
	previous := make([]platform.IntervalIdentity, 0, len(ids))
	reviews := map[string]bool{}
	for i, raw := range set.Records {
		var row map[string]json.RawMessage
		if json.Unmarshal(raw, &row) != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Invalid interval projection row")
		}
		var owner, source string
		var retired, review bool
		var interval platform.ObservationInterval
		for name, into := range map[string]any{p.Fields.Subject: &owner, p.Fields.Source: &source, p.Fields.Retired: &retired, p.Fields.Review: &review, p.Fields.Since: &interval.Since, p.Fields.Until: &interval.Until, p.Fields.OpenStart: &interval.OpenStart, p.Fields.OpenEnd: &interval.OpenEnd} {
			if len(row[name]) == 0 || string(row[name]) == "null" || json.Unmarshal(row[name], into) != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Invalid interval projection field {field}", name)
			}
		}
		if owner != subjectID || source != p.Source || retired {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Interval collection contains another source or retired identity")
		}
		previous = append(previous, platform.IntervalIdentity{ID: ids[i], Interval: interval})
		reviews[ids[i]] = review
	}
	plan, err := platform.ReconcileObservationIntervals(info.Type, previous, *proof.Intervals, func(i int) string { return receipt.GetChangeId() + "-" + step + "-" + strconv.Itoa(i) })
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	write := func(id, verb string, patch map[string]any) *kernel.Error {
		if failure := c.TxnTransport().TakeTxnStep(); failure != nil {
			return failure
		}
		if failure := reserve(info.Type + "/" + id); failure != nil {
			return failure
		}
		return b.ledger.ApplyTxnRecord(c, receipt, entity, verb, id, platform.Raw(patch), now)
	}
	for _, event := range plan.Events {
		if !event.Changed && (!event.Review || reviews[event.ID]) {
			continue
		}
		patch := map[string]any{p.Fields.Subject: subjectID, p.Fields.Source: p.Source, p.Fields.SourceVersion: proof.Definition.SourceVersion, p.Fields.SourceRevision: proof.Intervals.Revision, p.Fields.ProcessingVersion: proof.Intervals.Version, p.Fields.Since: event.Interval.Since, p.Fields.Until: event.Interval.Until, p.Fields.OpenStart: event.Interval.OpenStart, p.Fields.OpenEnd: event.Interval.OpenEnd, p.Fields.Retired: false}
		verb := "edit"
		if event.New {
			verb = "create"
			patch[p.Fields.Review] = event.Review
		} else if event.Review {
			patch[p.Fields.Review] = true
		}
		if failure := write(event.ID, verb, patch); failure != nil {
			return failure
		}
	}
	for _, id := range plan.Retired {
		if failure := write(id, "edit", map[string]any{p.Fields.Retired: true}); failure != nil {
			return failure
		}
	}
	for _, redirect := range plan.Redirects {
		for _, to := range redirect.To {
			if failure := c.TxnTransport().TakeTxnStep(); failure != nil {
				return failure
			}
			link := platform.TxnLink{Binding: p.Predecessors, Parent: platform.TxnRef{Type: info.Type, ID: redirect.From.Id}, Child: platform.TxnRef{Type: info.Type, ID: to.Id}}
			if failure := c.PutOwnedRelation(receipt, link, false, now); failure != nil {
				return failure
			}
		}
	}
	return nil
}
