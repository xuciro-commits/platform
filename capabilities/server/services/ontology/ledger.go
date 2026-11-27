package ontology

import (
	"fmt"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/resources/publication"
	"slices"
	"time"
)

func LedgerOf(o Object) *platform.LedgerSpec {
	if !o.Ledger {
		return nil
	}
	spec := &platform.LedgerSpec{ClosedBefore: o.ClosedBefore}
	for _, b := range o.Balances {
		spec.Balances = append(spec.Balances, b.LedgerBalance)
	}
	return spec
}

func NormalizeLedger(o *Object) error {
	if !o.Ledger {
		if len(o.Balances) > 0 || o.ClosedBefore != "" {
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
	}
	return nil
}

// LedgerBalances compiles generated read-only types, not user-writable objects.
func LedgerBalances(o Object) []platform.RecordType {
	if !o.Ledger {
		return nil
	}
	var out []platform.RecordType
	for _, b := range o.Balances {
		shape := Object{Name: o.Name + "." + b.Name, Namespace: o.Namespace, Title: b.Title, Access: b.Access}
		for _, name := range append(slices.Clone(b.Keys), b.Measures...) {
			i := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == name })
			if i < 0 {
				continue
			}
			f := o.Fields[i]
			f.ReadOnly = true
			f.From, f.FormulaExpr, f.Live = nil, nil, nil
			f.Formula, f.Default, f.When, f.OneOf = "", "", "", ""
			f.Write = nil
			shape.Fields = append(shape.Fields, f)
		}
		model := CompileObjectShape(shape, AppID, BuilderRole)
		_, model.Scope, _ = CompileAccess(shape, BuilderRole, UserRole)
		model.BalanceOf = o.Type()
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
