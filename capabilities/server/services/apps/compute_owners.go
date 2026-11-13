package applications

import "platformserver/platform"

func (ctx Context) ComputeOwners(root platform.AssetRef) ([]string, error) {
	_, graphs, err := ctx.Graphs(root)
	relevant := []string{}
	for _, graph := range graphs {
		for ref := range graph {
			if ref.Kind == platform.AssetCompute {
				relevant = append(relevant, ref.App)
			}
		}
	}
	return relevant, err
}
