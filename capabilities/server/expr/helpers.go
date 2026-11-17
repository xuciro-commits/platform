package expr

import "slices"

func Refs(expression Expr) []string {
	var out []string
	var visit func(Expr)
	visit = func(current Expr) {
		if current.Ref != "" && !slices.Contains(out, current.Ref) {
			out = append(out, current.Ref)
		}
		for _, argument := range current.Args {
			visit(argument)
		}
		if current.In != nil {
			visit(*current.In)
		}
	}
	visit(expression)
	return out
}

func MergeTypes(dst, src Type) Type {
	if dst.Kind == "" {
		return src
	}
	if dst.Fields == nil {
		dst.Fields = map[string]Type{}
	}
	for name, typ := range src.Fields {
		dst.Fields[name] = MergeTypes(dst.Fields[name], typ)
	}
	return dst
}
