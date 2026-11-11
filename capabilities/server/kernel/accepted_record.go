package kernel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"io"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
)

func DecodeAcceptedRecord(et *records.Type, draft *records.Store, result acceptedformat.Record, receipt *pb.ChangeRecord, check func(any) error) (reflect.Value, []byte, error) {
	value := reflect.New(et.Info().Go).Elem()
	decoded, err := records.DecodeFormattedImage(et.Info(), result.Row.Value, result.Row.Projection, draft.Current().Release(), result.Row.Format)
	if err != nil {
		return value, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value.Addr().Interface()); err != nil {
		return value, nil, fmt.Errorf("accepted record image: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return value, nil, fmt.Errorf("accepted record has trailing data")
	}
	if err := check(value.Interface()); err != nil {
		return value, nil, err
	}
	rec := records.Header(value)
	if rec.ID != result.Row.ID || rec.Revision != receipt.GetRevision() ||
		rec.Changed.By != receipt.GetSubmission().GetPrincipalId() ||
		rec.Changed.Change != receipt.GetChangeId() ||
		!rec.Changed.At.Equal(receipt.GetRecordedTime().AsTime()) {
		return value, nil, fmt.Errorf("accepted record image differs from receipt")
	}
	return value, decoded, nil
}

func InstallAcceptedRecord(draft *records.Store, et *records.Type, result acceptedformat.Record, receipt *pb.ChangeRecord, value reflect.Value, prior *records.Row) error {
	rec := records.Header(value)
	if result.Version >= 5 {
		if err := records.CheckHistoryPredecessor(result.Row, prior); err != nil {
			return err
		}
		before, err := records.Predecessor(result.Row.Type, result.Row.ID, prior)
		if err != nil || before != result.Row.Predecessor || prior != nil && !reflect.DeepEqual(records.Header(prior.Value).Created, rec.Created) || prior == nil && !reflect.DeepEqual(rec.Created, rec.Changed) {
			return fmt.Errorf("accepted record predecessor differs: %v", err)
		}
	} else {
		verb, _ := strings.CutPrefix(receipt.GetSubmission().GetSchema().GetName(), result.Row.Type+".")
		before := len(result.Row.History) - 1
		if verb == "create" && prior != nil || verb != "create" && prior == nil ||
			prior == nil && !reflect.DeepEqual(rec.Created, rec.Changed) ||
			prior == nil && before != 0 || prior != nil && (len(prior.RecordHistory()) != before ||
			before > 0 && !reflect.DeepEqual(prior.RecordHistory(), result.Row.History[:before]) ||
			!reflect.DeepEqual(records.Header(prior.Value).Created, rec.Created)) {
			return fmt.Errorf("accepted record is missing its predecessor")
		}
	}
	recovered := &records.Row{Value: value, Format: result.Row.Format, History: records.CopyHistory(result.Row.History)}
	if result.Version >= 5 {
		if err := records.AppendRowHistory(recovered, prior, result.Row.History); err != nil {
			return err
		}
	}
	recovered.RetainOriginal(result.Row.Value)
	et.SetRow(rec.ID, recovered)
	draft.TrackWrite(result.Row.Type + "/" + rec.ID)
	draft.RememberChange(result.App+"/"+receipt.GetChangeId(), result.Row.Type+"/"+rec.ID)
	if et.Knowledge() {
		draft.MarkDirty(result.Row.Type + "/" + rec.ID)
	}
	return nil
}

type RecordResult struct {
	Store          *records.Store
	App, Kind      string
	Receipt        *pb.ChangeRecord
	At             time.Time
	SchemaVersion  *platform.SchemaVersion
	Definition     *platform.DecisionDefinition
	Event          platform.AcceptedEventPlan
	Authorizations []acceptedformat.AuthorizationEvidence
	Unique         []persistence.StateUnique
}

func EncodeAcceptedRecord(in RecordResult) ([]byte, error) {
	typ, id := in.Receipt.GetSubmission().GetTarget().GetType(), in.Receipt.GetSubmission().GetTarget().GetId()
	image, err := func() (records.Image, error) {
		in.Store.Lock()
		defer in.Store.Unlock()
		et := in.Store.Type(typ)
		if et == nil || et.Info().App != in.App || et.Rows().Get(id) == nil {
			return records.Image{}, fmt.Errorf("decision has no declared record image")
		}
		if et.Rows().Get(id).Projection != nil {
			return records.Image{}, fmt.Errorf("versioned migration row requires the complete record batch")
		}
		var prior *records.Row
		if old := in.Store.Parent().Type(typ); old != nil {
			prior = old.Rows().Get(id)
		}
		image, err := records.ChangeImage(typ, id, et.Rows().Get(id), prior)
		if err == nil {
			image.Predecessor, err = records.Predecessor(typ, id, prior)
		}
		return image, err
	}()
	if err != nil {
		return nil, err
	}
	raw, err := protojson.Marshal(in.Receipt)
	if err != nil {
		return nil, err
	}
	hash, err := acceptedformat.SubmissionHash(in.Receipt.GetSubmission(), in.SchemaVersion)
	if err != nil {
		return nil, err
	}
	result := acceptedformat.Record{Version: acceptedformat.RecordVersion, Tenant: in.Receipt.GetSubmission().GetTenantId(),
		Definition: in.Definition,
		Kind:       in.Kind, App: in.App, At: in.At.UTC(), RequestHash: hash, Receipt: raw, SchemaVersion: in.SchemaVersion,
		Row:   image,
		Event: in.Event, Authorizations: slices.Clone(in.Authorizations)}
	result.Unique = in.Unique
	result.Digest, err = acceptedformat.DigestRecord(result)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, _, err := acceptedformat.DecodeRecord(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}
