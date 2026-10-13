package platform

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// BusinessTxn is the bounded, single-authority definition of ADR-0100's first
// slice. Rule guards and assignments use ADR-0101; other step kinds are refused.
type BusinessTxn struct {
	Subject string        `json:"subject"`
	Steps   []TxnStep     `json:"steps"`
	Guard   *Expr         `json:"guard,omitempty"`
	Message string        `json:"message,omitempty"`
	Reads   *RuleReadSpec `json:"reads,omitempty"`
}

type TxnStep struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Target   TxnRef    `json:"target,omitempty"`
	Changes  []TxnSet  `json:"changes,omitempty"`
	Sequence *Sequence `json:"sequence,omitempty"`
	When     *Expr     `json:"when,omitempty"`
}

type TxnRef struct {
	Type string `json:"type"`
	// ID is a fixed ID or $subject; a create leaves it empty for a stable ID.
	ID string `json:"id,omitempty"`
	// Via resolves a declared reference path from the subject for an update.
	Via string `json:"via,omitempty"`
}

// TxnSet retains the existing typed assignment sources. A transaction also
// accepts step.<name>, referring only to an earlier create ID or sequence.
type TxnSet struct {
	Field string `json:"field" field:"required" help:"A field of the object"`
	Expr  *Expr  `json:"expr,omitempty" type:"json" field:"aside"`
	From  string `json:"from,omitempty" field:"required" help:"An input's name, $me, $now, or =<a fixed value>" example:"to"`
}

func strictTxn(raw []byte, into any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "One transaction definition is required / 须提供一份事务定义")
	}
	return nil
}
func (t *BusinessTxn) UnmarshalJSON(raw []byte) error {
	type plain BusinessTxn
	return strictTxn(raw, (*plain)(t))
}
func (t *TxnStep) UnmarshalJSON(raw []byte) error {
	type plain TxnStep
	return strictTxn(raw, (*plain)(t))
}
func (t *TxnRef) UnmarshalJSON(raw []byte) error {
	type plain TxnRef
	return strictTxn(raw, (*plain)(t))
}

// ApplyTxnRecord uses the original generated action's catalog, row scope,
// field permissions, compute and validation, without another kernel decision.
// All rows carry the parent receipt and remain private until the host accepts
// its complete result. This is not an authoritative write entry point.
func (l *Ledger) ApplyTxnRecord(c Caller, r *pb.ChangeRecord, e Entity, verb, id string, payload json.RawMessage, now time.Time) *kernel.Error {
	if !c.Staging() || c.Replaying || c.rt.Probing() || c.App != l.authority || r.GetSubmission().GetAuthority() != c.App ||
		r.GetSubmission().GetTenantId() != l.tenant || r.GetSubmission().GetPrincipalId() != c.ID || r.GetChangeId() == "" || id == "" || !strings.HasPrefix(e.Type, c.App+".") ||
		(verb != "create" && verb != "edit") {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction step is outside its private authority / 事务步骤超出私有权威范围")
	}
	s := proto.Clone(r.GetSubmission()).(*pb.Submission)
	s.Schema = &pb.SchemaRef{Name: e.Type + "." + verb, Version: 1}
	s.Target = &pb.EntityRef{Type: e.Type, Id: id}
	s.Payload = payload
	a, ok := l.Catalog.Action(s.Schema.Name)
	if !ok || a.Target != e.Type || verb == "create" && !e.Standard.Create || verb == "edit" && !e.Standard.Edit {
		return Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "Transaction step action is unavailable / 事务步骤动作不可用")
	}
	c.Automation = false
	if !l.Catalog.DecideOn(c, s.Schema.Name, e.Type+"/"+id).Allow {
		return l.denied(c, s)
	}
	// The ordinary reference validator checks existence. Transaction assignment
	// also requires the referenced row to be readable in this private view.
	info, _ := Describe(c.App, e, func(_ reflect.Type) string { return "" })
	var given map[string]json.RawMessage
	if json.Unmarshal(payload, &given) != nil {
		return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction fields / 事务字段无效")
	}
	for name, raw := range given {
		if f, found := info.Field(name); found && f.Type == "reference" && f.Ref != "" {
			var ref string
			if json.Unmarshal(raw, &ref) != nil {
				return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction reference / 事务引用无效")
			}
			if ref != "" && !c.rt.Readable(c, f.Ref+"/"+ref) {
				return notFound()
			}
		}
	}
	apply, err := l.checked(c, s, now, func() (func(*pb.ChangeRecord), *kernel.Error) { return standard(c, e, verb, s, now) })()
	if err != nil {
		return err
	}
	apply(r)
	return nil
}
