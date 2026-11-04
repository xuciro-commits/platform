package ontology

import (
	"fmt"

	"platformserver/platform"
)

func FormulaExpr(fields []Field, f Field) (platform.Expr, error) {
	if f.FormulaExpr != nil {
		if f.Formula != "" {
			return platform.Expr{}, fmt.Errorf("field %s has two formulas", f.Name)
		}
		return *f.FormulaExpr, nil
	}
	types := map[string]string{}
	for _, operand := range fields {
		if operand.Formula == "" && operand.FormulaExpr == nil && (operand.Type == "integer" || operand.Type == "decimal" || operand.Type == "money") {
			types[operand.Name] = operand.Type
			if operand.Type == "money" {
				types[operand.Name] = "decimal"
			}
		}
	}
	e, err := platform.LowerFormula(f.Formula, types)
	if err != nil {
		return e, err
	}
	if f.Type == "integer" {
		e = platform.Expr{Op: "round", Kind: "integer", Args: []platform.Expr{e}}
	}
	return e, nil
}

func CheckFormulas(fields []Field) error {
	for _, f := range fields {
		if f.Formula != "" {
			if f.Type != "integer" && f.Type != "decimal" {
				return fmt.Errorf("field %s legacy formula needs a numeric destination", f.Name)
			}
			if _, err := FormulaExpr(fields, f); err != nil {
				return fmt.Errorf("field %s: %w", f.Name, err)
			}
		}
	}
	return nil
}

func FormulaOrder(fields []Field) ([]Field, error) {
	byName := map[string]Field{}
	for _, f := range fields {
		if f.Formula != "" || f.FormulaExpr != nil {
			byName[f.Name] = f
		}
	}
	visited, visiting := map[string]bool{}, map[string]bool{}
	out := []Field{}
	var walk func(string) error
	walk = func(name string) error {
		if visited[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("field %s formula dependency is cyclic", name)
		}
		f, ok := byName[name]
		if !ok {
			return nil
		}
		visiting[name] = true
		if f.FormulaExpr != nil {
			var refs func(platform.Expr) error
			refs = func(e platform.Expr) error {
				if _, ok := byName[e.Ref]; ok {
					if err := walk(e.Ref); err != nil {
						return err
					}
				}
				for _, a := range e.Args {
					if err := refs(a); err != nil {
						return err
					}
				}
				if e.In != nil {
					return refs(*e.In)
				}
				return nil
			}
			if err := refs(*f.FormulaExpr); err != nil {
				return err
			}
		}
		delete(visiting, name)
		visited[name] = true
		out = append(out, f)
		return nil
	}
	for _, f := range fields {
		if err := walk(f.Name); err != nil {
			return nil, err
		}
	}
	return out, nil
}
