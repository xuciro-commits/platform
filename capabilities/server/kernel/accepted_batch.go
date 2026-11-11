package kernel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func ApplyBatchRows(draft *records.Store, result acceptedformat.Batch, tenant string, owner func(app string) *platform.Ledger, ledgers map[string]*platform.Ledger, changes map[string]*pb.ChangeRecord, check func(any) error) error {
	draft.Lock()
	defer draft.Unlock()
	for _, image := range result.Rows {
		ledger := ledgers[image.App]
		if ledger == nil {
			ledger = owner(image.App)
		}
		if ledger == nil || !slices.ContainsFunc(ledger.Declarations(), func(d *pb.AuthorityDeclaration) bool {
			return d.GetTenantId() == tenant && d.GetAuthorityId() == image.App && d.GetDataClass() == image.Type
		}) {
			return fmt.Errorf("record batch has no authority for %s", image.Type)
		}
		et := draft.Type(image.Type)
		if et == nil || et.Info().App != image.App {
			return fmt.Errorf("record batch needs installed entity %s", image.Type)
		}
		prior := et.Rows().Get(image.ID)
		if prior != nil && !reflect.DeepEqual(prior.Extraction, image.Extraction) {
			return fmt.Errorf("record extraction origin changed")
		}
		before := ""
		baseHistory, revision := 0, uint32(0)
		if result.Version >= 8 {
			if err := records.CheckHistoryPredecessor(image.Image, prior); err != nil {
				return err
			}
			var err error
			before, err = records.Predecessor(image.Type, image.ID, prior)
			if err != nil {
				return err
			}
			if before != image.Before {
				return fmt.Errorf("record batch image predecessor differs")
			}
			if prior != nil {
				revision = records.Header(prior.Value).Revision
			}
		} else {
			if prior != nil {
				row, err := records.FullImage(image.Type, image.ID, prior)
				if err != nil {
					return err
				}
				before, err = platform.CanonicalDigest(row)
				if err != nil {
					return err
				}
				baseHistory, revision = len(prior.RecordHistory()), records.Header(prior.Value).Revision
			}
			if result.Version >= 2 {
				if image.HistoryBase != baseHistory {
					return fmt.Errorf("record batch history predecessor differs for %s/%s", image.Type, image.ID)
				}
				if prior != nil {
					image.History = append(records.CopyHistory(prior.RecordHistory()), image.History...)
				}
			}
			prefixEqual := true
			if result.Version == 1 && prior != nil && baseHistory > 0 && len(image.History) >= baseHistory {
				oldDigest, oldErr := platform.CanonicalDigest(prior.RecordHistory())
				savedDigest, savedErr := platform.CanonicalDigest(image.History[:baseHistory])
				prefixEqual = oldErr == nil && savedErr == nil && oldDigest == savedDigest
			}
			if before != image.Before || len(image.History) <= baseHistory || !prefixEqual {
				return fmt.Errorf("record batch predecessor differs for %s/%s (value=%t history=%d/%d prefix=%t)",
					image.Type, image.ID, before == image.Before, baseHistory, len(image.History), prefixEqual)
			}
		}
		value := reflect.New(et.Info().Go).Elem()
		decode, err := records.DecodeFormattedImage(et.Info(), image.Value, image.Projection, draft.Current().Release(), image.Format)
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(decode))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(value.Addr().Interface()); err != nil {
			return fmt.Errorf("record batch image: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("record batch image has trailing data")
		}
		if err := check(value.Interface()); err != nil {
			return err
		}
		rec := records.Header(value)
		var last *pb.ChangeRecord
		if len(image.Authorities) > 0 && len(image.Authorities) != len(image.History)-baseHistory {
			return fmt.Errorf("record batch history authorities differ")
		}
		for index, change := range image.History[baseHistory:] {
			app := image.App
			if len(image.Authorities) > 0 {
				app = image.Authorities[index]
			}
			r := changes[app+"/"+change.Change]
			if r == nil || change.Schema != r.GetSubmission().GetSchema().GetName() ||
				change.By != r.GetSubmission().GetPrincipalId() || !change.At.Equal(r.GetRecordedTime().AsTime()) {
				return fmt.Errorf("record batch has a foreign history suffix")
			}
			if r.GetSubmission().GetTarget().GetType() == image.Type &&
				r.GetSubmission().GetTarget().GetId() == image.ID {
				revision = r.GetRevision()
			}
			last = r
		}
		if rec.ID != image.ID || rec.Revision != revision || last == nil ||
			rec.Changed.Change != last.GetChangeId() || rec.Changed.By != last.GetSubmission().GetPrincipalId() ||
			!rec.Changed.At.Equal(last.GetRecordedTime().AsTime()) ||
			prior != nil && !reflect.DeepEqual(rec.Created, records.Header(prior.Value).Created) ||
			prior == nil && (rec.Created.Change != image.History[0].Change ||
				rec.Created.By != image.History[0].By || !rec.Created.At.Equal(image.History[0].At)) {
			return fmt.Errorf("record batch image differs from its decisions")
		}
		recovered := &records.Row{Value: value, Format: image.Format, History: records.CopyHistory(image.History), Projection: records.CloneProjection(image.Projection), Extraction: image.Extraction}
		if result.Version >= 8 {
			if err := records.AppendRowHistory(recovered, prior, image.History); err != nil {
				return err
			}
		}
		recovered.RetainOriginal(image.Value)
		et.SetRow(rec.ID, recovered)
		draft.TrackWrite(image.Type + "/" + rec.ID)
		for _, change := range image.History[baseHistory:] {
			draft.RememberChange(image.App+"/"+change.Change, image.Type+"/"+rec.ID)
		}
		if et.Knowledge() {
			draft.MarkDirty(image.Type + "/" + rec.ID)
		}
	}
	return nil
}
