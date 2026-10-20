package platformserver

import (
	"fmt"
	"platformserver/internal/acceptance"
	"platformserver/internal/records"
	"reflect"
)

// acceptedHistoryRef names an immutable prefix and this result's appended head.
// Format 1 hashes canonical JSON with an explicit domain and position.
type acceptedHistoryRef struct {
	Version int    `json:"version"`
	Base    int    `json:"base"`
	Prior   string `json:"prior"`
	Head    string `json:"head"`
}

func (r *row) historyDigest() (string, error) {
	if r == nil {
		return records.EmptyHistory, nil
	}
	if r.historyHead != "" {
		return r.historyHead, nil
	}
	head := records.EmptyHistory
	for i, h := range r.recordHistory() {
		var err error
		head, err = records.AppendHistory(head, i, jsonOf(h))
		if err != nil {
			return "", err
		}
	}
	return head, nil
}

func appendRowHistory(r, prior *row, changes []RecordChange) error {
	head, err := prior.historyDigest()
	if err != nil {
		return err
	}
	base := 0
	if prior != nil {
		base = prior.historyLength()
	}
	for i, h := range changes {
		head, err = records.AppendHistory(head, base+i, jsonOf(h))
		if err != nil {
			return err
		}
	}
	r.historyHead = head
	r.history = copyHistory(changes)
	if prior != nil {
		prefix := *prior
		prefix.value = reflect.Value{}
		prefix.original = nil
		prefix.projection = nil
		prefix.extraction = nil
		r.historyParent = &prefix
	}
	r.historyCount = base + len(changes)
	return nil
}

func acceptedRowChange(typ, id string, r, prior *row) (acceptedRow, error) {
	value, err := r.image()
	if err != nil {
		return acceptedRow{}, err
	}
	base := 0
	if prior != nil {
		base = prior.historyLength()
	}
	changes := r.historyAfter(base)
	before, err := prior.historyDigest()
	if err != nil {
		return acceptedRow{}, err
	}
	head, err := r.historyDigest()
	if err != nil {
		return acceptedRow{}, err
	}
	out := acceptedRow{Type: typ, ID: id, Value: value, Format: r.format, History: copyHistory(changes), Projection: cloneRecordProjection(r.projection), Extraction: r.extraction,
		HistoryRef: &acceptedHistoryRef{Version: 1, Base: base, Prior: before, Head: head}}
	return out, nil
}

func recordPredecessor(typ, id string, r *row) (string, error) {
	if r == nil {
		return "", nil
	}
	value, err := r.image()
	if err != nil {
		return "", err
	}
	head, err := r.historyDigest()
	if err != nil {
		return "", err
	}
	return acceptance.Digest(struct {
		Version    int               `json:"version"`
		Type       string            `json:"type"`
		ID         string            `json:"id"`
		Value      any               `json:"value"`
		Format     string            `json:"format,omitempty"`
		Count      int               `json:"count"`
		Head       string            `json:"head"`
		Projection *recordProjection `json:"projection,omitempty"`
		Extraction *extractionOrigin `json:"extraction,omitempty"`
	}{1, typ, id, value, r.format, r.historyLength(), head, r.projection, r.extraction})
}

func validateHistoryRef(image acceptedRow) error {
	ref := image.HistoryRef
	if ref == nil || ref.Version != 1 || ref.Base < 0 || len(image.History) == 0 {
		return fmt.Errorf("record history reference is incomplete")
	}
	head := ref.Prior
	for i, h := range image.History {
		var err error
		head, err = records.AppendHistory(head, ref.Base+i, jsonOf(h))
		if err != nil {
			return err
		}
	}
	if head != ref.Head {
		return fmt.Errorf("record history chain differs")
	}
	return nil
}

func checkHistoryPredecessor(image acceptedRow, prior *row) error {
	if err := validateHistoryRef(image); err != nil {
		return err
	}
	head, err := prior.historyDigest()
	if err != nil {
		return err
	}
	count := 0
	if prior != nil {
		count = prior.historyLength()
	}
	if image.HistoryRef.Base != count || image.HistoryRef.Prior != head {
		return fmt.Errorf("record history predecessor differs")
	}
	return nil
}

func acceptedRowAlreadyApplied(prior *row, image acceptedRow) bool {
	if image.HistoryRef != nil {
		head, err := prior.historyDigest()
		return err == nil && prior.historyLength() == image.HistoryRef.Base+len(image.History) && head == image.HistoryRef.Head
	}
	a, err := acceptance.Digest(prior.recordHistory())
	b, err2 := acceptance.Digest(image.History)
	return err == nil && err2 == nil && a == b
}
