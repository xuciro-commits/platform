package ontology

import (
	"fmt"
	"platformserver/platform"
	"strings"
)

// CheckStoredFormulas restricts persisted derived values to dependencies that
// the acceptance path refreshes. No fan-out updates to arbitrary references.
func CheckStoredFormulas(o Object, lookup func(string) (platform.RecordTypeInfo, bool)) error {
	self := RuleLookup(o, AppID, BuilderRole, lookup)
	for _, f := range o.Fields {
		if f.FormulaExpr == nil && f.Formula == "" {
			continue
		}
		e, err := FormulaExpr(o.Fields, f)
		if err != nil {
			return err
		}
		refuse := func(path string) error {
			return fmt.Errorf("object %s field %s stored formula reads non-owned or live path %s; use live, from, or a transaction guard", o.Type(), f.Name, path)
		}
		var walk func(platform.Expr, string, string) error
		checkField := func(typ, name string) error {
			info, ok := self(typ)
			if ok {
				if field, found := info.Field(name); found && field.Provenance == "live" {
					return refuse(typ + "." + name)
				}
			}
			return nil
		}
		walk = func(e platform.Expr, item, subject string) error {
			if e.Op == "path" {
				base := ""
				if len(e.Args) > 0 {
					base = e.Args[0].Ref
				}
				return refuse(base + "." + strings.Join(e.Path, "."))
			}
			if e.Window != nil {
				return refuse("window")
			}
			if e.Ref != "" {
				if name, ok := strings.CutPrefix(e.Ref, "item."); ok {
					if strings.Contains(name, ".") {
						return refuse(e.Ref)
					}
					if err := checkField(item, name); err != nil {
						return err
					}
				} else if err := checkField(subject, e.Ref); err != nil {
					return err
				}
			}
			if e.Agg != nil {
				if o.Reads == nil {
					return refuse(e.Agg.Source)
				}
				collection, ok := o.Reads.Collections[e.Agg.Source]
				if !ok || collection.Set == "" || collection.Query != "" {
					return refuse(e.Agg.Source)
				}
				var declared *platform.RecordSet
				for i := range o.Sets {
					set := &o.Sets[i]
					if set.Name == collection.Set && set.Object == collection.Object {
						declared = set
						break
					}
				}
				if declared == nil || declared.Binding != nil || declared.Direction != "inverse" || len(declared.Path) > 0 {
					return refuse(e.Agg.Source)
				}
				child, ok := self(declared.Object)
				if !ok {
					return refuse(e.Agg.Source)
				}
				owned := false
				for _, field := range child.Fields {
					if field.Ref == o.Type() && field.Inverse == declared.From {
						owned = true
						break
					}
				}
				if !owned {
					return refuse(e.Agg.Source)
				}
				item = declared.Object
				if len(e.Agg.Field) > 1 {
					return refuse(e.Agg.Source + "." + strings.Join(e.Agg.Field, "."))
				}
				if len(e.Agg.Field) == 1 {
					if err := checkField(item, e.Agg.Field[0]); err != nil {
						return err
					}
				}
				// Membership and ordering also affect the stored result.
				for _, order := range declared.Order {
					if err := walk(order.Expr, "", item); err != nil {
						return err
					}
				}
				for _, where := range declared.Where {
					if err := walk(where, "", item); err != nil {
						return err
					}
				}
			}
			for _, arg := range e.Args {
				if err := walk(arg, item, subject); err != nil {
					return err
				}
			}
			if e.In != nil {
				return walk(*e.In, item, subject)
			}
			return nil
		}
		if err := walk(e, "", o.Type()); err != nil {
			return err
		}
	}
	return nil
}
