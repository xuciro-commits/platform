// Package erplink connects an ERP outside the platform, such as SAP, to the
// apps that consume production.orders/1 (ADR-0024 7d): it provides the
// protocol as the ERP app does, so a plant has one path to any ERP. The ERP's
// planned orders arrive by polling (a K8 poll connector, exactly once by
// cursor); a confirmation goes to the ERP as an outbound effect (ADR-0014),
// and the ERP's answer comes back on the order, where the plant reads it.
package erplink

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"production"
)

const (
	ID        = "erplink"
	OrderType = "erplink.order"

	SchemaConfirm = OrderType + ".confirm"

	// EffectConfirmation is what the ERP receives: its endpoint is bound in Settings.
	EffectConfirmation = "confirmation"

	Connector = "erp"     // the ERP's service account: delivers pages of planned orders
	Planner   = "planner" // confirms orders to the ERP; a plant's automation does it as well

	Orders = "erplink-orders" // the protocol's read
)

// Translations: i18n/<language>.json, keyed by the English text (ADR-0023).
//
//go:embed i18n
var languageFiles embed.FS

var languages = platform.LoadLanguages(languageFiles, "i18n")

// Order is one of the ERP's planned orders; its ID is the ERP's.
type Order struct {
	platform.Record
	Product      string  `json:"product" field:"readonly,search"`
	Quantity     float64 `json:"quantity" field:"readonly"`
	Due          string  `json:"due,omitempty" field:"readonly" type:"date"`
	State        string  `json:"state" field:"readonly" choices:"released,sent,confirmed,refused,failed"`
	ShopOrder    string  `json:"shopOrder,omitempty" field:"readonly,search" title:"Shop order" help:"The plant's order its last confirmation named"`
	Yield        float64 `json:"yield,omitempty" field:"readonly" help:"Good units confirmed"`
	Scrap        float64 `json:"scrap,omitempty" field:"readonly" help:"Units scrapped"`
	Sent         int     `json:"sent,omitempty" field:"readonly" help:"Confirmations sent to the ERP"`
	Confirmation string  `json:"confirmation,omitempty" field:"readonly" help:"The ERP's number for the confirmation"`
	Detail       string  `json:"detail,omitempty" field:"readonly" help:"Why the ERP refused the confirmation, or why it did not arrive"`
}

// key names the order's current confirmation, so the ERP receives a
// confirmation sent again as a new message and a stale answer is recognised.
func (o Order) key() string { return fmt.Sprintf("%s#%d", o.ID, o.Sent) }

// Planned is a planned order as the ERP sends it.
type Planned struct {
	ID       string  `json:"id"`
	Product  string  `json:"product"`
	Quantity float64 `json:"quantity"`
	Due      string  `json:"due,omitempty"`
}

// Page is one poll of the ERP's planned orders.
type Page struct {
	CursorFrom string    `json:"cursorFrom"`
	CursorTo   string    `json:"cursorTo"`
	Orders     []Planned `json:"orders"`
}

// Confirmation is the body of the effect the ERP receives.
type Confirmation struct {
	Order     string  `json:"order"`
	ShopOrder string  `json:"shopOrder"`
	Product   string  `json:"product"`
	Yield     float64 `json:"yield"`
	Scrap     float64 `json:"scrap"`
}

func Entities() []platform.Entity {
	return []platform.Entity{{Type: OrderType, Title: "ERP order", Model: Order{}, Synonyms: "planned order, production order",
		Description: "A planned order of the ERP outside, polled from it; the plant makes it and its confirmation goes back to the ERP.",
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "released",
			States: []platform.State{
				{Name: "released", Title: "Released", Tone: "info", Description: "The plant may make it"},
				{Name: "sent", Title: "Sent", Tone: "warning", Description: "A confirmation is on its way to the ERP"},
				{Name: "confirmed", Title: "Confirmed", Tone: "success", Description: "The ERP accepted the confirmation"},
				{Name: "refused", Title: "Refused", Tone: "danger", Description: "The ERP refused the confirmation; it may be confirmed again"},
				{Name: "failed", Title: "Failed", Tone: "danger", Description: "The confirmation did not reach the ERP; it may be confirmed again"}}}}}
}

func Actions() *platform.Catalog {
	return platform.NewCatalog(platform.Action{Schema: SchemaConfirm, Target: OrderType, Capability: "confirmations", Title: "Confirm to the ERP",
		Description: "Send what the plant made and scrapped for a planned order to the ERP, which answers with its confirmation number or refuses it.",
		Payload:     production.Protocol().Actions[0].Payload, Roles: []string{Planner}})
}

// App is the adapter in one tenant; the host keeps its records (ADR-0016).
type App struct {
	mu     sync.Mutex
	tenant string
	ledger *platform.Ledger
}

func New(tenant string) *App {
	return &App{tenant: tenant, ledger: platform.NewLedger(tenant, ID, Actions(), OrderType)}
}

// Poll is the connector the ERP's service account delivers pages through,
// expected every ten minutes.
func Poll(member string) *pb.ConnectorDescriptor {
	return &pb.ConnectorDescriptor{ConnectorId: member, Direction: pb.ConnectorDirection_CONNECTOR_DIRECTION_POLL,
		DataClasses: []string{OrderType}, Heartbeat: durationpb.New(10 * time.Minute)}
}

func (a *App) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: "ERP link", Version: "1", Actions: a.ledger.Catalog, Entities: Entities(), Languages: languages,
		Reads: []string{Orders}, Inputs: map[string]bool{"planned-orders": true},
		Emits: []platform.EffectKind{{Name: EffectConfirmation, Title: "Production order confirmation", Irreversible: true,
			Description: "The yield and scrap of a planned order the plant made, sent to the ERP (SAP production order confirmation); the ERP answers with its confirmation number."}},
		Provides: []platform.Provision{{Protocol: production.Protocol(),
			Actions: map[string]string{"confirm": SchemaConfirm}, Reads: map[string]string{"orders": Orders}}}}
}

func (a *App) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }
func (a *App) Snapshot() (json.RawMessage, error)       { return a.ledger.Snapshot() }
func (a *App) Restore(raw json.RawMessage) error        { return a.ledger.Restore(raw) }

func fail(code pb.ErrorCode) *kernel.Error { return &kernel.Error{Code: code} }

// Input takes a page of the ERP's planned orders: new ones are released, known
// ones take the ERP's product, quantity and due date and keep where they stand.
func (a *App) Input(c platform.Caller, name string, body []byte, now time.Time) (any, *kernel.Error) {
	var page Page
	switch {
	case name != "planned-orders":
		return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
	case c.Role() != Connector && !c.Replaying:
		return nil, fail(pb.ErrorCode_ERROR_CODE_POLICY_DENIED)
	case json.Unmarshal(body, &page) != nil:
		return nil, fail(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	for _, p := range page.Orders {
		if p.ID == "" || p.Product == "" || p.Quantity <= 0 {
			return nil, fail(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := c.Deliver(OrderType, page.CursorFrom, page.CursorTo, now); err != nil {
		return nil, err
	}
	for _, p := range page.Orders {
		o, known := platform.Get[Order](c, p.ID)
		if !known {
			o = Order{Record: platform.Record{ID: p.ID}, State: "released"}
		}
		o.Product, o.Quantity, o.Due = p.Product, p.Quantity, p.Due
		if err := c.PutAt(now, "planned order", o); err != nil {
			return nil, err
		}
	}
	return len(page.Orders), nil
}

// Submit receives a confirmation: of an order not sent or confirmed, no more
// than its quantity made and scrapped; accepted, it goes to the ERP.
func (a *App) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		o, known := platform.Get[Order](c, s.GetTarget().GetId())
		var in production.Confirmation
		switch {
		case s.GetSchema().GetName() != SchemaConfirm:
			return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
		case !known:
			return nil, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
		case o.State == "sent" || o.State == "confirmed":
			return nil, fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
		case json.Unmarshal(s.GetPayload(), &in) != nil || in.ShopOrder == "" || in.Yield < 0 || in.Scrap < 0 ||
			in.Yield+in.Scrap <= 0 || in.Yield+in.Scrap > o.Quantity:
			return nil, fail(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		return func(r *pb.ChangeRecord) {
			o.State, o.ShopOrder, o.Yield, o.Scrap, o.Confirmation, o.Detail = "sent", in.ShopOrder, in.Yield, in.Scrap, "", ""
			o.Sent++
			body := Confirmation{Order: o.ID, ShopOrder: in.ShopOrder, Product: o.Product, Yield: in.Yield, Scrap: in.Scrap}
			if n, _ := c.Emit(EffectConfirmation, o.key(), OrderType+"/"+o.ID, body, now); n == 0 {
				o.State, o.Detail = "failed", "no endpoint is bound to ERP confirmations"
			}
			c.Put(r, o)
		}, nil
	})
}

// Answer records how the ERP answered the order's current confirmation (D4);
// the answer to one since sent again changes nothing.
func (a *App) Answer(c platform.Caller, e platform.Effect, out platform.Outcome, now time.Time) *kernel.Error {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, _, _ := strings.Cut(e.Key, "#")
	o, known := platform.Get[Order](c, id)
	if e.Event != ID+"/"+EffectConfirmation || !known || e.Key != o.key() {
		return nil
	}
	var body struct {
		Confirmation string `json:"confirmation"`
		Error        string `json:"error"`
	}
	json.Unmarshal(out.Answer, &body)
	o.State, o.Confirmation, o.Detail = map[string]string{"delivered": "confirmed", "rejected": "refused"}[e.State], body.Confirmation, out.Detail
	if o.State == "" {
		o.State = "failed"
	}
	if body.Error != "" {
		o.Detail = body.Error
	}
	if o.State == "confirmed" {
		o.Detail = ""
	}
	return c.PutAt(now, "ERP answer", o)
}

func (a *App) Read(c platform.Caller, name string) (any, *kernel.Error) {
	if name != Orders {
		return nil, fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	out := []production.Order{}
	for _, o := range platform.Records[Order](c) {
		out = append(out, production.Order{ID: o.ID, Number: o.ID, Product: o.Product, Quantity: o.Quantity, Due: o.Due, State: o.State,
			ShopOrder: o.ShopOrder, Confirmation: o.Confirmation, Detail: o.Detail})
	}
	return out, nil
}
