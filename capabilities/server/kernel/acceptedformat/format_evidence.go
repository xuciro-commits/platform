package acceptedformat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/journal"
	"platformserver/kernel/records"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

type ChangePlan struct {
	Rows          []InputRow
	Member        string
	At            time.Time
	Changes       json.RawMessage
	States        []State
	Control       *Control
	Intents       []platform.Effect
	Deliveries    []ConnectorDelivery
	Notices       *Notices
	Sequences     map[string]int
	SequenceBases map[string]int
	Unique        []journal.StateUnique
	CheckUnique   bool
}

func (r Input) ChangePlan() ChangePlan {
	return ChangePlan{Rows: r.Rows, Member: r.Member, At: r.At, Changes: r.Changes,
		States: r.States, Deliveries: r.Deliveries, Notices: r.Notices, Unique: r.Unique, CheckUnique: r.Version >= 2 && len(r.Changes) == 0}
}

func (r Effect) ChangePlan() ChangePlan {
	return ChangePlan{Rows: r.Rows, Member: r.Member, At: r.At, Changes: r.Changes,
		States: r.States, Control: r.Control, Intents: r.Intents, Notices: r.Notices, Unique: r.Unique, CheckUnique: r.Version >= 4}
}

func (r Batch) ChangePlan() ChangePlan {
	return ChangePlan{At: r.At, States: r.States, Control: r.Control, Intents: r.Intents,
		Deliveries: r.Deliveries, Notices: r.Notices, Sequences: r.Sequences,
		SequenceBases: r.SequenceBases, Unique: r.Unique, CheckUnique: r.Version >= 8}
}

type State struct {
	App    string          `json:"app"`
	Before string          `json:"before"`
	Image  json.RawMessage `json:"image"`
}

type Notices struct {
	Base    int                     `json:"base"`
	Seq     int                     `json:"seq"`
	Before  string                  `json:"before"`
	Notices []platform.Notification `json:"notices"`
	Effects []platform.Effect       `json:"effects,omitempty"`
}

type Control struct {
	Changes []ControlChange `json:"changes"`
}

type ControlChange struct {
	Kind   string          `json:"kind"`
	Before string          `json:"before"`
	Image  json.RawMessage `json:"image"`
}

type ConnectorDelivery struct {
	Connector string    `json:"connector"`
	DataClass string    `json:"dataClass"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	At        time.Time `json:"at"`
	Before    string    `json:"before"`
}

type Observation struct {
	App  string          `json:"app"`
	Body json.RawMessage `json:"body"`
}

type OperationCancellation struct {
	Before    platform.Effect `json:"before"`
	After     platform.Effect `json:"after"`
	PriorWork json.RawMessage `json:"priorWork,omitempty"`
	Work      json.RawMessage `json:"work,omitempty"`
}

type Publication struct {
	Format string          `json:"format,omitempty"`
	Schema string          `json:"schema"`
	Image  json.RawMessage `json:"image"`
}

type Decision struct {
	Definition  *platform.DecisionDefinition `json:"definition,omitempty"`
	Receipt     json.RawMessage              `json:"receipt"`
	Event       platform.AcceptedEventPlan   `json:"event"`
	Publication *Publication                 `json:"publication,omitempty"`
}

type BatchRow struct {
	records.Image
	App         string   `json:"app"`
	Before      string   `json:"before,omitempty"`
	Authorities []string `json:"authorities,omitempty"`
	HistoryBase int      `json:"historyBase,omitempty"`
}

type InputRow struct {
	records.Image
	App    string `json:"app"`
	Before string `json:"before,omitempty"`
}

type CallbackRow struct {
	Row    InputRow        `json:"row"`
	Source platform.Effect `json:"source"`
}

type AuthorizationEvidence struct {
	Request  platform.AuthzRequest  `json:"request"`
	Decision platform.AuthzDecision `json:"decision"`
	Roles    []string               `json:"roles"`
}

func ValidateAuthorizationEvidence(tenant string, evidence []AuthorizationEvidence) error {
	order := []string{"tenant", "principal", "deny", "verb", "row", "fields", "derived", "markings", "allow"}
	for _, item := range evidence {
		q, decision := item.Request, item.Decision
		if q.Principal == "" || q.Tenant != tenant || !slices.Contains([]string{"read", "write", "create", "archive", "action", "derive", "export", "protocol"}, q.Kind) || decision.Reason == "" || len(decision.Layers) == 0 || len(decision.Layers) > len(order) {
			return fmt.Errorf("saved authorization identity or layers are invalid")
		}
		for i, layer := range decision.Layers {
			if layer.Name != order[i] || layer.Reason == "" || !layer.Allowed && i != len(decision.Layers)-1 {
				return fmt.Errorf("saved authorization layer order is invalid")
			}
		}
		last := decision.Layers[len(decision.Layers)-1]
		if decision.Allow != last.Allowed || decision.Allow && len(decision.Layers) != len(order) {
			return fmt.Errorf("saved authorization outcome differs from its layers")
		}
	}
	return nil
}

const MaxHops = 100

func SubmissionHash(s *pb.Submission, versions ...*platform.SchemaVersion) (string, error) {
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(s)
	if err != nil {
		return "", err
	}
	if len(versions) > 0 && versions[0] != nil {
		binding, err := json.Marshal(versions[0])
		if err != nil {
			return "", err
		}
		raw = append(append(raw, 0), binding...)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func ValidateSubmissionSchemaVersion(sub *pb.Submission, binding *platform.SchemaVersion) error {
	if binding == nil {
		return nil
	}
	if err := binding.Check(); err != nil {
		return err
	}
	if sub.GetSchema().GetVersion() != binding.Family {
		return fmt.Errorf("submission family differs from its saved schema binding")
	}
	return nil
}

func ValidateBusinessFact(app string, f platform.BusinessRefusalFact) error {
	if !slices.Contains([]string{"block", "flag", "route", "rejected"}, f.Level) || f.Code == "" || len(f.Code) > 64 || len(f.Message) > 1024 || f.Message == "" || f.Member == "" || f.At.IsZero() || !strings.HasPrefix(f.Subject, app+".") || !strings.Contains(f.Subject, "/") || !strings.HasPrefix(f.Action, app+".") || len(f.Subject) > 512 || len(f.Original) > 64<<10 || !json.Valid(f.Original) || len(f.Inputs) > 64 || len(f.Translations) > 1 {
		return fmt.Errorf("invalid business refusal evidence")
	}
	for language, message := range f.Translations {
		if language != "zh-CN" || len(message) > 1024 {
			return fmt.Errorf("invalid business refusal translation")
		}
	}
	for name, raw := range f.Inputs {
		if len(name) > 128 || len(raw) > 64<<10 || !json.Valid(raw) {
			return fmt.Errorf("invalid business refusal input")
		}
	}
	if f.Escalation != nil {
		if err := f.Escalation.Check(); err != nil {
			return err
		}
	}
	if f.Level == "route" && (f.Route == nil || f.Route.Action == "") {
		return fmt.Errorf("business refusal has no declared route")
	}

	return nil
}

func ValidateTxnIterations(app string, iterations []platform.TxnIteration) error {
	total, steps := 0, map[string]bool{}
	for _, iteration := range iterations {
		owner, key := app, iteration.Step
		if app == "" {
			var change string
			owner, change, _ = strings.Cut(iteration.Decision, "/")
			if owner == "" || change == "" || strings.Contains(change, "/") {
				return fmt.Errorf("transaction iteration has no decision identity")
			}
			key = iteration.Decision + "/" + iteration.Step
		} else if iteration.Decision != "" {
			return fmt.Errorf("legacy transaction iteration carries a decision identity")
		}
		hash, err := hex.DecodeString(iteration.Definition)
		if err != nil || len(hash) != 32 || iteration.Step == "" || iteration.Set == "" || steps[key] || !strings.HasPrefix(iteration.Object, owner+".") {
			return fmt.Errorf("transaction iteration witness is invalid")
		}
		steps[key] = true
		ids := map[string]bool{}
		for _, id := range iteration.IDs {
			if id == "" || ids[id] {
				return fmt.Errorf("transaction iteration identities must be unique")
			}
			ids[id] = true
		}
		total += len(iteration.IDs)
		if total > platform.TxnMaxIterations {
			return fmt.Errorf("transaction has %d iterations; limit is %d", total, platform.TxnMaxIterations)
		}
	}
	return nil
}
