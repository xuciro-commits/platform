package records

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/acceptance"
	"platformserver/platform"
	"reflect"
	"slices"
	"time"
)

// A Row and its nested values are immutable once stored. Writers replace it;
// readers that expose editable values copy them at the API boundary.
type Row struct {
	Value         reflect.Value // the entity struct, a private copy
	HistoryParent *Row
	HistoryHead   string
	Format        string
	History       []RecordChange
	LoadHistory   func(int) []RecordChange
	HistoryCount  int
	// Recovery bytes preserve an accepted predecessor's identity when typed
	// decoding normalises its definition. The next mutation writes only the
	// current format; these bytes have no writable or executable API.
	Original   json.RawMessage
	Projection *Projection
	Extraction *Extraction
}

type Extraction struct {
	Migration string `json:"migration"`
	Source    string `json:"source"`
	Step      int    `json:"step"`
}

// A migration retains the source release's read image while its target facts
// are accepted in batches. Only that exact shape can select this projection.
type Projection struct {
	Format string          `json:"format,omitempty"`
	From   string          `json:"from"`
	Shape  string          `json:"shape"`
	Value  json.RawMessage `json:"value"`
	Prior  []Projection    `json:"prior,omitempty"`
}

func CloneProjection(p *Projection) *Projection {
	if p == nil {
		return nil
	}
	copy := &Projection{Format: p.Format, From: p.From, Shape: p.Shape, Value: slices.Clone(p.Value)}
	for _, prior := range p.Prior {
		copy.Prior = append(copy.Prior, *CloneProjection(&prior))
	}
	return copy
}

func ProjectionFor(p *Projection, release string) *Projection {
	if p == nil {
		return nil
	}
	if p.From == release {
		return p
	}
	for i := range p.Prior {
		if p.Prior[i].From == release {
			return &p.Prior[i]
		}
	}
	return nil
}

// Source values remain facts of their release. Only the common record header
// follows subsequent accepted writes, so every image names the same record.
func RefreshProjection(p *Projection, value any) error {
	if p == nil {
		return nil
	}
	var header map[string]json.RawMessage
	if json.Unmarshal(rowJSON(value), &header) != nil {
		return fmt.Errorf("versioned record header is invalid")
	}
	refresh := func(image *Projection) error {
		var fields map[string]json.RawMessage
		if json.Unmarshal(image.Value, &fields) != nil {
			return fmt.Errorf("versioned source image is invalid")
		}
		for _, name := range []string{"id", "revision", "created", "changed", "archived", "versionPins"} {
			if value, present := header[name]; present {
				fields[name] = value
			} else {
				delete(fields, name)
			}
		}
		raw, err := json.Marshal(fields)
		image.Value = raw
		return err
	}
	if err := refresh(p); err != nil {
		return err
	}
	for i := range p.Prior {
		if err := refresh(&p.Prior[i]); err != nil {
			return err
		}
	}
	return nil
}

func DecodeImage(info platform.EntityInfo, value json.RawMessage, projection *Projection, release string) (json.RawMessage, error) {
	if projection == nil {
		return value, nil
	}
	shape, err := acceptance.Digest(info.Fields)
	if err != nil {
		return nil, err
	}
	var actual platform.Record
	if json.Unmarshal(value, &actual) != nil {
		return nil, fmt.Errorf("versioned record identity is invalid")
	}
	seen := map[string]bool{}
	images := append([]Projection{*projection}, projection.Prior...)
	for i, image := range images {
		if image.From == "" || image.Shape == "" || len(image.Value) == 0 || seen[image.From] || i > 0 && len(image.Prior) != 0 {
			return nil, fmt.Errorf("incomplete or duplicate versioned record projection")
		}
		seen[image.From] = true
		var view platform.Record
		if json.Unmarshal(image.Value, &view) != nil || !reflect.DeepEqual(actual, view) {
			return nil, fmt.Errorf("versioned record projection identity differs")
		}
	}
	if selected := ProjectionFor(projection, release); selected != nil && shape == selected.Shape {
		return selected.Value, nil
	}
	return value, nil
}

func (r *Row) Image() (json.RawMessage, error) {
	if len(r.Original) > 0 {
		return slices.Clone(r.Original), nil
	}
	return json.Marshal(r.Value.Interface())
}

func (r *Row) RetainOriginal(raw json.RawMessage) {
	decoded, err := json.Marshal(r.Value.Interface())
	before, beforeErr := acceptance.Digest(raw)
	after, afterErr := acceptance.Digest(json.RawMessage(decoded))
	if err == nil && beforeErr == nil && afterErr == nil && before != after {
		r.Original = slices.Clone(raw)
	}
}

// RecordChange is one decision's change to a record: the fields it set.
type RecordChange struct {
	Change string        `json:"change"`
	Schema string        `json:"schema"`
	By     string        `json:"by"`
	At     time.Time     `json:"at"`
	Fields []FieldChange `json:"fields"`
}

type FieldChange struct {
	Field  string          `json:"field"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

type Image struct {
	Type        string          `json:"type"`
	Format      string          `json:"format,omitempty"`
	HistoryRef  *HistoryRef     `json:"historyRef,omitempty"`
	Predecessor string          `json:"predecessor,omitempty"`
	ID          string          `json:"id"`
	Value       json.RawMessage `json:"value"`
	History     []RecordChange  `json:"history"`
	Projection  *Projection     `json:"projection,omitempty"`
	Extraction  *Extraction     `json:"extraction,omitempty"`
}

func CopyHistory(history []RecordChange) []RecordChange {
	out := slices.Clone(history)
	for i := range out {
		out[i].Fields = slices.Clone(history[i].Fields)
		for j := range out[i].Fields {
			out[i].Fields[j].Before = slices.Clone(history[i].Fields[j].Before)
			out[i].Fields[j].After = slices.Clone(history[i].Fields[j].After)
		}
	}
	return out
}

func Header(v reflect.Value) *platform.Record {
	return v.Field(0).Addr().Interface().(*platform.Record)
}

func FullImage(typ, id string, r *Row) (Image, error) {
	value, err := r.Image()
	return Image{Type: typ, ID: id, Value: value, History: CopyHistory(r.RecordHistory()), Projection: CloneProjection(r.Projection), Extraction: r.Extraction}, err
}

func (r *Row) RecordHistory() []RecordChange { return r.HistoryAfter(0) }

func (r *Row) HistoryLength() int {
	if r.LoadHistory != nil || r.HistoryParent != nil || r.HistoryCount > 0 {
		return r.HistoryCount
	}
	return len(r.History)
}
func (r *Row) HistoryAfter(start int) []RecordChange {
	if start >= r.HistoryLength() {
		return nil
	}
	if start < 0 {
		start = 0
	}
	if r.LoadHistory != nil {
		return r.LoadHistory(start)
	}
	if r.HistoryParent == nil {
		return r.History[start:]
	}
	base := r.HistoryParent.HistoryLength()
	if start >= base {
		return r.History[start-base:]
	}
	// Walk immutable suffix segments once. Recursively concatenating prefixes
	// would copy a long in-memory history again at every retained revision.
	out := make([]RecordChange, 0, r.HistoryLength()-start)
	var suffixes [][]RecordChange
	for r.HistoryParent != nil && start < r.HistoryParent.HistoryLength() {
		suffixes = append(suffixes, r.History)
		r = r.HistoryParent
	}
	if r.LoadHistory != nil {
		out = append(out, r.LoadHistory(start)...)
	} else if r.HistoryParent != nil {
		out = append(out, r.History[start-r.HistoryParent.HistoryLength():]...)
	} else {
		out = append(out, r.History[start:]...)
	}
	for i := len(suffixes) - 1; i >= 0; i-- {
		out = append(out, suffixes[i]...)
	}
	return out
}

// HistoryRef names an immutable prefix and this result's appended head.
// Format 1 hashes canonical JSON with an explicit domain and position.
type HistoryRef struct {
	Version int    `json:"version"`
	Base    int    `json:"base"`
	Prior   string `json:"prior"`
	Head    string `json:"head"`
}

func (r *Row) HistoryDigest() (string, error) {
	if r == nil {
		return EmptyHistory, nil
	}
	if r.HistoryHead != "" {
		return r.HistoryHead, nil
	}
	head := EmptyHistory
	for i, h := range r.RecordHistory() {
		var err error
		head, err = AppendHistory(head, i, rowJSON(h))
		if err != nil {
			return "", err
		}
	}
	return head, nil
}

func AppendRowHistory(r, prior *Row, changes []RecordChange) error {
	head, err := prior.HistoryDigest()
	if err != nil {
		return err
	}
	base := 0
	if prior != nil {
		base = prior.HistoryLength()
	}
	for i, h := range changes {
		head, err = AppendHistory(head, base+i, rowJSON(h))
		if err != nil {
			return err
		}
	}
	r.HistoryHead = head
	r.History = CopyHistory(changes)
	if prior != nil {
		prefix := *prior
		prefix.Value = reflect.Value{}
		prefix.Original = nil
		prefix.Projection = nil
		prefix.Extraction = nil
		r.HistoryParent = &prefix
	}
	r.HistoryCount = base + len(changes)
	return nil
}

func ChangeImage(typ, id string, r, prior *Row) (Image, error) {
	value, err := r.Image()
	if err != nil {
		return Image{}, err
	}
	base := 0
	if prior != nil {
		base = prior.HistoryLength()
	}
	changes := r.HistoryAfter(base)
	before, err := prior.HistoryDigest()
	if err != nil {
		return Image{}, err
	}
	head, err := r.HistoryDigest()
	if err != nil {
		return Image{}, err
	}
	out := Image{Type: typ, ID: id, Value: value, Format: r.Format, History: CopyHistory(changes), Projection: CloneProjection(r.Projection), Extraction: r.Extraction,
		HistoryRef: &HistoryRef{Version: 1, Base: base, Prior: before, Head: head}}
	return out, nil
}

func Predecessor(typ, id string, r *Row) (string, error) {
	if r == nil {
		return "", nil
	}
	value, err := r.Image()
	if err != nil {
		return "", err
	}
	head, err := r.HistoryDigest()
	if err != nil {
		return "", err
	}
	return acceptance.Digest(struct {
		Version    int         `json:"version"`
		Type       string      `json:"type"`
		ID         string      `json:"id"`
		Value      any         `json:"value"`
		Format     string      `json:"format,omitempty"`
		Count      int         `json:"count"`
		Head       string      `json:"head"`
		Projection *Projection `json:"projection,omitempty"`
		Extraction *Extraction `json:"extraction,omitempty"`
	}{1, typ, id, value, r.Format, r.HistoryLength(), head, r.Projection, r.Extraction})
}

func ValidateHistoryRef(image Image) error {
	ref := image.HistoryRef
	if ref == nil || ref.Version != 1 || ref.Base < 0 || len(image.History) == 0 {
		return fmt.Errorf("record history reference is incomplete")
	}
	head := ref.Prior
	for i, h := range image.History {
		var err error
		head, err = AppendHistory(head, ref.Base+i, rowJSON(h))
		if err != nil {
			return err
		}
	}
	if head != ref.Head {
		return fmt.Errorf("record history chain differs")
	}
	return nil
}

func CheckHistoryPredecessor(image Image, prior *Row) error {
	if err := ValidateHistoryRef(image); err != nil {
		return err
	}
	head, err := prior.HistoryDigest()
	if err != nil {
		return err
	}
	count := 0
	if prior != nil {
		count = prior.HistoryLength()
	}
	if image.HistoryRef.Base != count || image.HistoryRef.Prior != head {
		return fmt.Errorf("record history predecessor differs")
	}
	return nil
}

func ImageAlreadyApplied(prior *Row, image Image) bool {
	if image.HistoryRef != nil {
		head, err := prior.HistoryDigest()
		return err == nil && prior.HistoryLength() == image.HistoryRef.Base+len(image.History) && head == image.HistoryRef.Head
	}
	a, err := acceptance.Digest(prior.RecordHistory())
	b, err2 := acceptance.Digest(image.History)
	return err == nil && err2 == nil && a == b
}

// DecodeFormattedImage keeps publication projections and owner record formats
// separate. Selecting an older release selects that projection's saved format;
// the model owner alone describes how that format becomes its current model.
func DecodeFormattedImage(info platform.EntityInfo, value json.RawMessage, projection *Projection, release, format string) (json.RawMessage, error) {
	decoded, err := DecodeImage(info, value, projection, release)
	if err != nil {
		return nil, err
	}
	if selected := ProjectionFor(projection, release); selected != nil {
		shape, err := acceptance.Digest(info.Fields)
		if err != nil {
			return nil, err
		}
		if shape == selected.Shape {
			format = selected.Format
		}
	}
	return platform.DecodeRecordImage(info, format, decoded)
}

// PruneImage keeps the fields a declaration still has. A type declared again
// may have fewer fields than the rows it carries: the removed ones are left
// out of the current value, and the record identity is kept as it was.
func PruneImage(info platform.EntityInfo, raw json.RawMessage) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	kept := map[string]bool{"id": true, "revision": true, "created": true, "changed": true, "archived": true, "versionPins": true}
	for _, field := range info.Fields {
		kept[field.Name] = true
	}
	for name := range object {
		if !kept[name] {
			delete(object, name)
		}
	}
	return json.Marshal(object)
}

func rowJSON(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }
