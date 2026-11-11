package kernel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

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
		revision := uint32(0)
		if err := records.CheckHistoryPredecessor(image.Image, prior); err != nil {
			return err
		}
		before, err := records.Predecessor(image.Type, image.ID, prior)
		if err != nil {
			return err
		}
		if before != image.Before {
			return fmt.Errorf("record batch image predecessor differs")
		}
		if prior != nil {
			revision = records.Header(prior.Value).Revision
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
		if len(image.Authorities) > 0 && len(image.Authorities) != len(image.History) {
			return fmt.Errorf("record batch history authorities differ")
		}
		for index, change := range image.History {
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
		if err := records.AppendRowHistory(recovered, prior, image.History); err != nil {
			return err
		}
		recovered.RetainOriginal(image.Value)
		et.SetRow(rec.ID, recovered)
		draft.TrackWrite(image.Type + "/" + rec.ID)
		for _, change := range image.History {
			draft.RememberChange(image.App+"/"+change.Change, image.Type+"/"+rec.ID)
		}
		if et.Knowledge() {
			draft.MarkDirty(image.Type + "/" + rec.ID)
		}
	}
	return nil
}

func SealBatch(store *records.Store, result acceptedformat.Batch, seen map[string]bool, writers map[string][]string) ([]byte, error) {
	var err error
	store.Lock()
	defer store.Unlock()
	store.Parent().Lock()
	defer store.Parent().Unlock()
	for _, ref := range slices.Sorted(store.Writes()) {
		if !seen[ref] {
			continue
		}
		typ, id, ok := strings.Cut(ref, "/")
		et := store.Type(typ)
		if !ok || et == nil || et.Rows().Get(id) == nil {
			return nil, fmt.Errorf("batch includes an undeclared record")
		}
		var prior *records.Row
		if old := store.Parent().Type(typ); old != nil {
			prior = old.Rows().Get(id)
		}
		image, err := records.ChangeImage(typ, id, et.Rows().Get(id), prior)
		if err != nil {
			return nil, fmt.Errorf("batch image %s/%s: %w", typ, id, err)
		}
		row := acceptedformat.BatchRow{Image: image, App: et.Info().App, Authorities: slices.Clone(writers[ref]), HistoryBase: image.HistoryRef.Base}
		row.Before, err = records.Predecessor(typ, id, prior)
		if err != nil {
			return nil, fmt.Errorf("batch predecessor %s/%s: %w", typ, id, err)
		}
		result.Rows = append(result.Rows, row)
	}
	result.Digest, err = acceptedformat.DigestBatch(result)
	if err != nil {
		return nil, fmt.Errorf("batch digest: %w", err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, _, err := acceptedformat.DecodeBatch(raw); err != nil {
		return nil, err
	}
	return raw, nil
}
