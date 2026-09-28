package pms

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

func (*Hotel) AcceptedInputs() []string { return []string{"channel-bookings"} }

func (*Hotel) DecodeAcceptedInput(raw json.RawMessage) (any, error) {
	var record pb.ChangeRecord
	err := protojson.Unmarshal(raw, &record)
	if err == nil && (record.GetChangeId() == "" || record.GetSubmission() == nil) {
		err = fmt.Errorf("accepted channel booking has no decision")
	}
	return &record, err
}

// A channel observation must be staged alongside its reservation decision.
// The shared ledger is forked by the caller's Runtime; the hotel's fact log
// alone is copied here. Room declarations are immutable across this input.
func (h *Hotel) ForkAcceptedState() (platform.App, error) {
	raw, err := h.AcceptedState()
	if err != nil {
		return nil, err
	}
	fork := New(h.tenant, nil)
	fork.entities = slices.Clone(h.entities)
	fork.ledger = h.ledger
	if err := fork.ApplyAcceptedState(raw); err != nil {
		return nil, err
	}
	return fork, nil
}

func (h *Hotel) AcceptedState() (json.RawMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return platform.SnapshotFacts(h.facts, h.tenant)
}

func (h *Hotel) ValidateAcceptedState(raw json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var packed []json.RawMessage
	if err := decoder.Decode(&packed); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("channel facts have trailing data")
	}
	records, err := platform.Unprotos[*pb.FactRecord](raw)
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, record := range records {
		if record.GetFactId() == "" || ids[record.GetFactId()] ||
			record.GetFact().GetTenantId() != h.tenant ||
			record.GetFact().GetSchema().GetName() != channelMessageSchema ||
			record.GetRecordedTime() == nil {
			return fmt.Errorf("accepted channel fact is invalid")
		}
		ids[record.GetFactId()] = true
	}
	return nil
}

func (h *Hotel) ApplyAcceptedState(raw json.RawMessage) error {
	if err := h.ValidateAcceptedState(raw); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return platform.RestoreFacts(h.facts, h.tenant, raw)
}

func (h *Hotel) HasAcceptedFact(tenant, id string) bool {
	return tenant == h.tenant && slices.ContainsFunc(h.facts.Records(tenant),
		func(record *pb.FactRecord) bool { return record.GetFactId() == id })
}

var _ platform.AcceptedFactApp = (*Hotel)(nil)
var _ platform.AcceptedInputApp = (*Hotel)(nil)
var _ platform.AcceptedStateApp = (*Hotel)(nil)
