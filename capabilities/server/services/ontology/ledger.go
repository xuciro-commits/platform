package ontology

import (
	"fmt"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/resources/publication"
	"slices"
	"strings"
	"time"
)

func LedgerOf(o Object) *platform.LedgerSpec {
	if !o.Ledger {
		return nil
	}
	spec := &platform.LedgerSpec{ClosedBefore: o.ClosedBefore, ClosedBeforeRecord: o.ClosedBeforeRecord}
	for _, b := range o.Balances {
		spec.Balances = append(spec.Balances, b.LedgerBalance)
	}
	return spec
}

func NormalizeLedger(o *Object) error {
	if !o.Ledger {
		if len(o.Balances) > 0 || o.ClosedBefore != "" || o.ClosedBeforeRecord != nil || o.Depletion != nil {
			return fmt.Errorf("balances and closedBefore require ledger: true")
		}
		return nil
	}
	if !slices.ContainsFunc(o.Fields, func(f Field) bool { return f.Name == "on" }) {
		o.Fields = append(o.Fields, Field{Name: "on", Title: "Business date", Type: "date", Required: true, Default: "today"})
	}
	if !slices.ContainsFunc(o.Fields, func(f Field) bool { return f.Name == "reverses" }) {
		o.Fields = append(o.Fields, Field{Name: "reverses", Title: "Reverses", Type: "reference", Ref: o.Type(), ReadOnly: true})
	}
	if !slices.ContainsFunc(o.Fields, func(f Field) bool { return f.Name == "reversed" }) {
		o.Fields = append(o.Fields, Field{Name: "reversed", Title: "Reversed", Type: "boolean", ReadOnly: true})
	}
	return nil
}

func CheckLedger(o Object, engine platform.RuleEngine) error {
	if !o.Ledger {
		return NormalizeLedger(&o)
	}
	if len(o.Balances) == 0 {
		return fmt.Errorf("ledger %s needs a balance declaration", o.Type())
	}
	if o.ClosedBefore != "" {
		if _, err := time.Parse(time.DateOnly, o.ClosedBefore); err != nil {
			return fmt.Errorf("ledger closedBefore must be a date")
		}
	}
	if r := o.ClosedBeforeRecord; r != nil {
		if r.Type == "" || r.Type == o.Type() || r.ID == "" || r.Field == "" {
			return fmt.Errorf("ledger closedBeforeRecord needs another type, record id and date field")
		}
	}
	if len(o.DataSources) > 0 || len(o.Records) > 0 || len(o.States) > 0 || len(o.Actions) > 0 || o.CreateTransaction != nil || o.Extends != "" || o.Versioning != nil {
		return fmt.Errorf("ledger %s facts are posted, not sourced, seeded, extended or transitioned", o.Type())
	}
	fields := map[string]Field{}
	for _, f := range o.Fields {
		fields[f.Name] = f
	}
	on, reverse := fields["on"], fields["reverses"]
	marker := fields["reversed"]
	if marker.Type != "boolean" || !marker.ReadOnly || marker.Required || marker.Live != nil || marker.Default != "" || marker.From != nil || marker.FormulaExpr != nil {
		return fmt.Errorf("ledger reversed is a host-owned read projection")
	}
	if on.Type != "date" || !on.Required || on.Live != nil || on.FormulaExpr != nil || on.From != nil || on.ReadOnly {
		return fmt.Errorf("ledger on must be a required editable business date")
	}
	if reverse.Type != "reference" || reverse.Ref != o.Type() || !reverse.ReadOnly || reverse.Required || reverse.Default != "" || reverse.From != nil || reverse.FormulaExpr != nil || reverse.Live != nil {
		return fmt.Errorf("ledger reverses is a host-owned reference")
	}
	names := map[string]bool{}
	for _, b := range o.Balances {
		if !publication.ValidName(b.Name) || b.Title == "" || names[b.Name] || len(b.Measures) == 0 {
			return fmt.Errorf("ledger %s balance needs unique name, title and measures", o.Type())
		}
		names[b.Name] = true
		seen := map[string]bool{}
		refs := map[string]platform.ExprType{}
		for _, name := range append(slices.Clone(b.Keys), b.Measures...) {
			f, ok := fields[name]
			if !ok || seen[name] || !f.Required || f.Live != nil || name == "on" || name == "reverses" || name == "reversed" {
				return fmt.Errorf("ledger balance %s invalid key or measure %s", b.Name, name)
			}
			seen[name] = true
			if slices.Contains(b.Measures, name) && !slices.Contains([]string{"integer", "decimal", "money"}, f.Type) {
				return fmt.Errorf("ledger balance %s measure %s must be numeric", b.Name, name)
			}
			if slices.Contains(b.Keys, name) && !slices.Contains([]string{"text", "choice", "reference", "member", "date", "integer"}, f.Type) {
				return fmt.Errorf("ledger balance %s key %s has unsupported type", b.Name, name)
			}
			refs[name] = platform.ExprType{Kind: platform.RuleFieldKind(f.Type), Object: f.Ref}
		}
		for _, a := range b.Access {
			if a.Create || a.Edit || a.Archive || (a.Read != "all" && a.Read != "none") {
				return fmt.Errorf("ledger balance %s access is read-only and must be all or none", b.Name)
			}
		}
		for _, c := range b.Constraints {
			if c.Name == "" || c.Kind != "expression" || c.Expr == nil || c.Expr.Kind != "boolean" || c.Reads != nil || c.Message == "" {
				return fmt.Errorf("ledger balance %s needs a local boolean constraint", b.Name)
			}
			if _, err := engine.CompileExpr(*c.Expr, platform.ExprSchema{Refs: refs}); err != nil {
				return fmt.Errorf("ledger balance %s constraint %s: %w", b.Name, c.Name, err)
			}
		}
		for _, bf := range b.Fields {
			if !publication.ValidName(bf.Name) || bf.Title == "" || seen[bf.Name] || bf.Live == nil || bf.From != nil || bf.FormulaExpr != nil || bf.Formula != "" || bf.Default != "" || bf.Required {
				return fmt.Errorf("ledger balance %s field %s must be a live derived field", b.Name, bf.Name)
			}
			if platform.RuleFieldKind(bf.Type) != platform.RuleFieldKind(bf.Live.Kind) {
				return fmt.Errorf("ledger balance %s field %s live result type differs", b.Name, bf.Name)
			}
			if _, err := engine.CompileExpr(*bf.Live, platform.ExprSchema{Refs: refs}); err != nil {
				return fmt.Errorf("ledger balance %s field %s: %w", b.Name, bf.Name, err)
			}
			seen[bf.Name] = true
		}
	}
	if d := o.Depletion; d != nil {
		qf, qok := fields[d.Quantity]
		af, aok := fields[d.Amount]
		if d.Ledger == "" || d.Balance == "" || d.Origin == "" || d.Consumer == "" || !qok || !aok || !qf.Required || !af.Required ||
			!slices.Contains([]string{"integer", "decimal"}, qf.Type) || !slices.Contains([]string{"integer", "decimal", "money"}, af.Type) {
			return fmt.Errorf("ledger %s depletion requires target ledger, balance, origin, consumer and numeric quantity/amount fields", o.Type())
		}
		if d.When != nil && d.When.Kind != "boolean" {
			return fmt.Errorf("ledger %s depletion condition must be boolean", o.Type())
		}
	}
	return nil
}

// CheckLedgerCloseSource verifies the declared source shape. Its actual value
// is read only during acceptance, never captured into the ledger plan.
func CheckLedgerCloseSource(o Object, lookup func(string) (platform.RecordTypeInfo, bool)) error {
	if r := o.ClosedBeforeRecord; r != nil {
		info, ok := lookup(r.Type)
		f, found := info.Field(r.Field)
		if !ok || !found || f.Type != "date" || f.Provenance == "live" || info.Ledger != nil || info.BalanceOf != "" {
			return fmt.Errorf("ledger close source must be a stored date on another record type")
		}
	}
	return nil
}

// LedgerBalances compiles generated read-only types, not user-writable objects.
func LedgerBalances(o Object) []platform.RecordType {
	var out []platform.RecordType
	if !o.Ledger {
		return append(out, InFlightProjections(o)...)
	}
	for _, b := range o.Balances {
		shape := Object{Name: o.Name + "." + b.Name, Namespace: o.Namespace, Title: b.Title, Access: b.Access, Protocols: slices.Clone(b.Protocols)}
		for _, name := range append(slices.Clone(b.Keys), b.Measures...) {
			i := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == name })
			if i < 0 {
				continue
			}
			f := o.Fields[i]
			if slices.Contains(b.Measures, name) {
				f.Required = false
			}
			f.ReadOnly = true
			f.From, f.FormulaExpr, f.Live = nil, nil, nil
			f.Formula, f.Default, f.When, f.OneOf = "", "", "", ""
			f.Write = nil
			shape.Fields = append(shape.Fields, f)
		}
		for _, bf := range b.Fields {
			bf.ReadOnly = true
			bf.Write = nil
			shape.Fields = append(shape.Fields, bf)
		}
		model := CompileObjectShape(shape, AppID, BuilderRole)
		_, model.Scope, _ = CompileAccess(shape, BuilderRole, UserRole)
		model.Protocols = ObjectProvisions(shape, model, nil)
		model.BalanceOf = o.Type()
		out = append(out, model)
	}
	return append(out, InFlightProjections(o)...)
}

func InFlightProjections(o Object) []platform.RecordType {
	var out []platform.RecordType
	for _, decl := range o.Protocols {
		if decl.InFlight == nil {
			continue
		}
		var fields []Field
		seen := map[string]bool{}
		addField := func(f Field) {
			if f.Name == "" || seen[f.Name] {
				return
			}
			seen[f.Name] = true
			f.Required, f.ReadOnly = false, true
			f.From, f.FormulaExpr, f.Live = nil, nil, nil
			f.Formula, f.Default, f.When, f.OneOf = "", "", "", ""
			f.Write = nil
			if decl.InFlight.By != "" && f.Name == decl.InFlight.By && decl.InFlight.Ref != "" {
				f.Type, f.Ref, f.Inverse = "reference", decl.InFlight.Ref, decl.InFlight.Inverse
			}
			fields = append(fields, f)
		}
		verbs := make([]string, 0, len(decl.Actions))
		for verb := range decl.Actions {
			verbs = append(verbs, verb)
		}
		slices.Sort(verbs)
		for _, verb := range verbs {
			target := decl.Actions[verb]
			if target == "create" || target == "edit" {
				for _, f := range o.Fields {
					if !f.ReadOnly && f.Live == nil && f.Formula == "" && f.FormulaExpr == nil {
						addField(f)
					}
				}
				continue
			}
			if idx := slices.IndexFunc(o.Actions, func(a Action) bool { return a.Name == target }); idx >= 0 {
				for _, in := range o.Actions[idx].Inputs {
					refType := in.RefType
					if refType == "" {
						if ofIdx := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == in.Name }); ofIdx >= 0 {
							refType = o.Fields[ofIdx].RefType
						}
					}
					addField(Field{Name: in.Name, Title: in.Title, Type: in.Type, Choices: in.Choices, Ref: in.Ref, RefType: refType})
				}
			}
		}
		for i := range fields {
			if fields[i].Type == "text" && fields[i].RefType == "" && strings.HasSuffix(fields[i].Name, "id") && len(fields[i].Name) > 2 {
				candidate := strings.TrimSuffix(fields[i].Name, "id") + "type"
				if seen[candidate] {
					fields[i].RefType = candidate
				}
			}
		}
		addField(Field{Name: "action", Title: "Action", Type: "text"})
		addField(Field{Name: "state", Title: "State", Type: "choice", Choices: "pending:Pending,held:Held,retrying:Retrying,unknown:Unknown"})
		addField(Field{Name: "error", Title: "Error", Type: "text"})
		var roAccess []Access
		for _, a := range o.Access {
			if a.Read == "all" || a.Read == "none" {
				roAccess = append(roAccess, Access{Role: a.Role, Read: a.Read})
			} else if a.Read != "" {
				roAccess = append(roAccess, Access{Role: a.Role, Read: "all"})
			}
		}
		shape := Object{Name: o.Name + ".inflight", Namespace: o.Namespace, Title: o.Title + " In-Flight", Fields: fields, Access: roAccess}
		model := CompileObjectShape(shape, AppID, BuilderRole)
		_, model.Scope, _ = CompileAccess(shape, BuilderRole, UserRole)
		model.InFlight = fmt.Sprintf("%s/%d", decl.Name, decl.Version)
		out = append(out, model)
	}
	return out
}

func InstallLedgerBalances(c platform.Caller, o Object, host protocol.BuildHost, authority platform.Authority, installed func(platform.RecordType)) error {
	for _, model := range LedgerBalances(o) {
		if err := authority.Extend([]string{model.Type}, nil); err != nil {
			return err
		}
		if err := host.Install(c, model, nil); err != nil {
			return err
		}
		installed(model)
	}
	return nil
}
