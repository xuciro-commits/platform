package kernel

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"reflect"
	"slices"
	"strings"
	"time"

	"platformserver/kernel/acceptedformat"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

func (d *Decision) SavedUniqueChanges() []persistence.StateUnique {
	d.Records.Lock()
	defer d.Records.Unlock()
	return UniqueReplacements(d.Records.UniqueChanges())
}

func (d *Decision) FlowVersions() map[string]int {
	if private, ok := d.PrivateApps[platform.FlowApp].(platform.Processes); ok {
		return private.Versions()
	}
	return nil
}

func (d *Decision) SavedAuthorizations() ([]acceptedformat.AuthorizationEvidence, error) {
	var saved []acceptedformat.AuthorizationEvidence
	seen := map[string]bool{}
	for _, item := range d.Authorizations {
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		key := string(raw)
		if !seen[key] {
			seen[key] = true
			saved = append(saved, item)
		}
	}
	return saved, nil
}

func (d *Decision) Get(c platform.Caller, typ reflect.Type, id string) (any, bool) {
	return d.Records.Get(c, typ, id)
}

func (d *Decision) Check(c platform.Caller, entity any) *kernel.Error {
	at := d.At
	if at.IsZero() {
		at = time.Now()
	}
	return d.Records.Check(c, entity, at)
}

func (d *Decision) CaptureBusinessRefusal(c platform.Caller, receipt *pb.ChangeRecord, fact platform.BusinessRefusalFact) *kernel.Error {
	if !c.Staging() || c.Replaying || receipt.GetSubmission().GetAuthority() != c.App || receipt.GetSubmission().GetPrincipalId() != c.ID || fact.Member != c.ID || !fact.At.Equal(d.At) || fact.Subject != Target(receipt.GetSubmission()) || fact.Action != receipt.GetSubmission().GetSchema().GetName() || fact.Level == "block" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Business refusal evidence must belong to its original decision")
	}
	if err := acceptedformat.ValidateBusinessFact(c.App, fact); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if len(d.BusinessFacts) >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal evidence exceeds its declared bounds")
	}
	d.BusinessFacts = append(d.BusinessFacts, acceptedformat.CloneBusinessFact(fact))
	return nil
}

func (d *Decision) RefuseBusinessCondition(c platform.Caller, code, message string, original any, inputs map[string]any, at time.Time) *kernel.Error {
	sub := d.CurrentSubmission
	if sub == nil || sub.GetAuthority() != c.App || sub.GetPrincipalId() != c.ID {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{message}", message)
	}
	fact := platform.BusinessRefusalFact{Level: "block", Code: code, Message: message, Subject: Target(sub), Action: sub.GetSchema().GetName(), Member: c.ID, At: at.UTC(), Original: platform.Raw(original), Inputs: map[string]json.RawMessage{}}
	for name, v := range inputs {
		fact.Inputs[name] = platform.Raw(v)
	}
	return c.RefuseBusiness(fact)
}

func (d *Decision) PrepareBusinessRecord(c platform.Caller, entity any, at time.Time) (any, *kernel.Error) {
	d.Records.Lock()
	et := d.Records.Of(c, reflect.TypeOf(entity))
	if et == nil {
		d.Records.Unlock()
		return entity, nil
	}
	info := et.Info()
	value := records.Copy(info.Go, entity)
	record := records.Header(value)
	before := et.Rows().Get(record.ID)
	if before != nil {
		record.VersionPins = slices.Clone(records.Header(before.Value).VersionPins)
	} else {
		record.VersionPins = nil
	}
	d.Records.Unlock()
	if before == nil && len(info.VersionPins) > 0 && info.Lifecycle != nil && records.BusinessField(info, value, info.Lifecycle.Field) == "" {
		field, _ := info.Field(info.Lifecycle.Field)
		value.FieldByIndex(field.Index).SetString(info.Lifecycle.Initial)
	}
	refuse := func(message string) (any, *kernel.Error) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message)
	}
	if versioning := info.Versioning; versioning != nil {
		if record.Archived {
			return refuse("Business versions are retired by valid-until and cannot be archived")
		}
		field, _ := info.Field(versioning.ValidFrom)
		from, until := records.BusinessField(info, value, versioning.ValidFrom), records.BusinessField(info, value, versioning.ValidUntil)
		if _, err := platform.EffectiveAt(field.Type, from, until, from); err != nil {
			return refuse(err.Error())
		}
		if before != nil && (records.BusinessField(info, value, versioning.Key) != records.BusinessField(info, before.Value, versioning.Key) || records.BusinessField(info, value, versioning.Version) != records.BusinessField(info, before.Value, versioning.Version)) {
			return refuse("The business key and version are retained")
		}
	}
	for _, field := range info.Fields {
		if field.Type != "reference" {
			continue
		}
		id := records.BusinessField(info, value, field.Name)
		if id == "" {
			continue
		}
		var source *records.Type
		var selected *records.Row
		func() {
			d.Records.Lock()
			defer d.Records.Unlock()
			source = d.Records.Type(field.Ref)
			if source != nil {
				selected = source.Rows().Get(id)
			}
		}()
		if source == nil || source.Info().Versioning == nil {
			continue
		}
		if selected == nil || records.Header(selected.Value).Archived {
			return refuse("The selected business version is unavailable")
		}
		pinAt := slices.IndexFunc(record.VersionPins, func(pin platform.RecordVersionPin) bool { return pin.Reference == field.Name })
		if pinAt >= 0 {
			pin := record.VersionPins[pinAt]
			versionField, ok := info.Field(pin.VersionField)
			if !ok || pin.Source != field.Ref+"/"+id || string(platform.Raw(value.FieldByIndex(versionField.Index).Interface())) != string(pin.Version) {
				return refuse("A pinned business version cannot be changed")
			}
			continue
		}
		versioning := source.Info().Versioning
		bindingAt := slices.IndexFunc(info.VersionPins, func(binding platform.VersionPinBinding) bool { return binding.Reference == field.Name })
		ready := versioning.Pin == "on_create" && before == nil
		if strings.HasPrefix(versioning.Pin, "on_state:") && info.Lifecycle != nil {
			ready = records.BusinessField(info, value, info.Lifecycle.Field) == strings.TrimPrefix(versioning.Pin, "on_state:")
		}
		if before != nil && records.BusinessField(info, before.Value, field.Name) == id && (bindingAt < 0 || !ready) {
			continue
		}
		clock := d.BusinessQuery(source.Info(), platform.Query{}, c.Member, at).BusinessTime.At
		from, until := records.BusinessField(source.Info(), selected.Value, versioning.ValidFrom), records.BusinessField(source.Info(), selected.Value, versioning.ValidUntil)
		timeField, _ := source.Info().Field(versioning.ValidFrom)
		active, err := platform.EffectiveAt(timeField.Type, from, until, clock)
		if err != nil || !active {
			return refuse("New references require a currently effective business version")
		}
		if bindingAt < 0 || versioning.Pin == "none" {
			continue
		}
		if !ready {
			continue
		}
		binding := info.VersionPins[bindingAt]
		versionField, _ := source.Info().Field(versioning.Version)
		if !field.ReadsAny(c.RolesIn(info.App), c.Rules()) || !versionField.ReadsAny(c.RolesIn(source.Info().App), c.Rules()) {
			return refuse("The selected business version is not readable")
		}
		if _, readErr := c.ReadRecord(source.Info().Type, id, at); readErr != nil {
			return refuse("The selected business version is not readable")
		}
		raw := platform.Raw(selected.Value.FieldByIndex(versionField.Index).Interface())
		target, _ := info.Field(binding.Version)
		if json.Unmarshal(raw, value.FieldByIndex(target.Index).Addr().Interface()) != nil {
			return refuse("The pinned version has an incompatible field type")
		}
		record.VersionPins = append(record.VersionPins, platform.RecordVersionPin{Reference: field.Name, VersionField: binding.Version, Source: field.Ref + "/" + id, Version: raw, At: at})
	}
	for _, pin := range record.VersionPins {
		field, ok := info.Field(pin.Reference)
		if !ok || field.Ref+"/"+records.BusinessField(info, value, pin.Reference) != pin.Source {
			return refuse("A pinned business version cannot be changed")
		}
	}
	return value.Interface(), nil
}

func (d *Decision) PrepareBusinessVersions(c platform.Caller, record any, at time.Time) *kernel.Error {
	value := reflect.ValueOf(record)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Business version preparation requires a record")
	}
	prepared, err := d.PrepareBusinessRecord(c, value.Elem().Interface(), at)
	if err == nil {
		value.Elem().Set(reflect.ValueOf(prepared))
	}
	return err
}

func (d *Decision) Find(c platform.Caller, typ reflect.Type, q platform.Query) ([]any, int, *kernel.Error) {
	d.Records.Lock()
	defer d.Records.Unlock()
	et := d.Records.Of(c, typ)
	if et == nil {
		return nil, 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if len(q.Sort) == 0 {
		q.Sort = []string{"id"}
	}
	q = d.BusinessQuery(et.Info(), q, c.Member, d.At)
	page, total, err := d.Records.Find(et, q, nil)
	out := make([]any, len(page))
	for i, v := range page {
		out[i] = records.Copy(typ, v.Interface()).Interface()
	}
	return out, total, err
}

func (d *Decision) SavedCallbackRows(published map[string]bool) ([]acceptedformat.CallbackRow, error) {
	var rows []acceptedformat.CallbackRow
	for _, ref := range slices.Sorted(d.Records.Writes()) {
		if published[ref] {
			continue
		}
		callback, ok := d.CallbackWrites[ref]
		if !ok {
			return nil, fmt.Errorf("batch event omits a written record outside an effect callback")
		}
		direct, err := d.CaptureDirectRows(callback.Source.App, []string{ref}, nil, "effect answer")
		if err != nil || len(direct) != 1 {
			return nil, fmt.Errorf("callback row cannot be captured: %v", err)
		}
		historyCount := len(direct[0].History)
		if direct[0].HistoryRef != nil {
			historyCount += direct[0].HistoryRef.Base
		}
		if historyCount != callback.History {
			return nil, fmt.Errorf("callback row has a write outside its source callback")
		}
		if err := d.DirectRows(direct, "app:"+callback.Source.App, d.At); err != nil {
			return nil, err
		}
		rows = append(rows, acceptedformat.CallbackRow{Row: direct[0], Source: callback.Source})
	}
	return rows, nil
}

func (d *Decision) Put(c platform.Caller, r *pb.ChangeRecord, entity any) *kernel.Error {
	var before *records.Row
	var info platform.EntityInfo
	var ref string
	func() {
		d.Records.Lock()
		defer d.Records.Unlock()
		et := d.Records.Of(c, reflect.TypeOf(entity))
		if et != nil {
			info = et.Info()
			id := records.Header(records.Copy(et.Info().Go, entity)).ID
			before, ref = et.Rows().Get(id), et.Info().Type+"/"+id
		}
	}()
	prepared, err := d.PrepareBusinessRecord(c, entity, d.At)
	if err == nil {
		err = d.Records.Put(c, r, prepared)
	}
	if err == nil {
		if d.ConstraintWrites == nil {
			d.ConstraintWrites = map[string]ConstraintWrite{}
		}
		if _, exists := d.ConstraintWrites[ref]; !exists {
			d.ConstraintWrites[ref] = ConstraintWrite{Caller: c, Before: before}
		}
		err = d.ConstraintCheck().Record(c, info, d.Records.Type(info.Type).Rows().Get(strings.TrimPrefix(ref, info.Type+"/")), before)
	}
	if err != nil && d.Failure == nil {
		d.Failure = err
	}
	if err == nil {
		var history int
		func() {
			d.Records.Lock()
			defer d.Records.Unlock()
			et := d.Records.Of(c, reflect.TypeOf(entity))
			id := records.Header(records.Copy(et.Info().Go, entity)).ID
			history = et.Rows().Get(id).HistoryLength()
		}()
		if origin := d.CallbackOrigin; origin != nil && origin.App == c.App && c.ID == "app:"+origin.App &&
			r.GetChangeId() == "" && r.GetSubmission().GetAuthority() == "" && r.GetSubmission().GetPrincipalId() == c.ID &&
			r.GetSubmission().GetSchema().GetName() != "" && r.GetRecordedTime().AsTime().Equal(d.At) {
			if d.CallbackWrites == nil {
				d.CallbackWrites = map[string]CallbackWrite{}
			}
			d.CallbackWrites[ref] = CallbackWrite{Source: *origin, History: history}
		}
		app := r.GetSubmission().GetAuthority()
		d.Writers[ref] = append(d.Writers[ref], app)
		key := app + "/" + r.GetChangeId()
		if !slices.Contains(d.Changed[key], ref) {
			d.Changed[key] = append(d.Changed[key], ref)
		}
	}
	return err
}

func (d *Decision) CaptureDirectRows(owner string, refs []string, exclude map[string]bool, source string) ([]acceptedformat.InputRow, error) {
	d.Records.Lock()
	defer d.Records.Unlock()
	d.Records.Parent().Lock()
	defer d.Records.Parent().Unlock()
	if refs == nil {
		refs = slices.Sorted(d.Records.Writes())
	}
	var rows []acceptedformat.InputRow
	for _, ref := range refs {
		if exclude[ref] {
			continue
		}
		typ, id, ok := strings.Cut(ref, "/")
		et := d.Records.Type(typ)
		if !ok || et == nil || et.Info().App != owner || et.Rows().Get(id) == nil {
			return nil, fmt.Errorf("%s writes a foreign or undeclared record", source)
		}
		var prior *records.Row
		if old := d.Records.Parent().Type(typ); old != nil {
			prior = old.Rows().Get(id)
		}
		image, err := records.ChangeImage(typ, id, et.Rows().Get(id), prior)
		if err != nil {
			return nil, err
		}
		row := acceptedformat.InputRow{Image: image, App: owner}
		row.Before, err = records.Predecessor(typ, id, prior)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (d *Decision) CheckFinalConstraints() *kernel.Error {
	if d.Failure == nil {
		d.Failure = d.ConstraintCheck().Final()
	}
	return d.Failure
}

func (d *Decision) ConstraintCheck() ConstraintCheck {
	return ConstraintCheck{Records: d.Records, At: d.At, Writes: d.ConstraintWrites, Definitions: d.Rules.Definitions(),
		RuleQuery: d.Rules.RuleQuery, Binding: d.Rules.Binding, Trace: d.Rules.Trace()}
}
