package protocol

import (
	"fmt"
	"platformserver/platform"
	"slices"
	"strings"
)

func FlowClosureKey(ref platform.AssetRef, version int) string {
	return fmt.Sprintf("%s@%d", ref, version)
}

func CloneFlowClosures(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, list := range in {
		if len(list) > 0 {
			out[k] = slices.Clone(list)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func MergeFlowClosures(dst, src map[string][]string) map[string][]string {
	out := CloneFlowClosures(dst)
	for k, list := range src {
		if k == "" {
			continue
		}
		for _, digest := range list {
			if digest == "" {
				continue
			}
			if out == nil {
				out = map[string][]string{}
			}
			if slices.Contains(out[k], digest) {
				continue
			}
			if len(out[k]) >= 64 {
				out[k] = append(slices.Clone(out[k][1:]), digest)
			} else {
				out[k] = append(out[k], digest)
			}
		}
	}
	return out
}

func HasFlowClosure(closures map[string][]string, ref platform.AssetRef, version int, dependencies string) bool {
	if len(closures) == 0 || dependencies == "" {
		return false
	}
	return slices.Contains(closures[FlowClosureKey(ref, version)], dependencies)
}

func ValidateFlowClosures(closures map[string][]string) error {
	if len(closures) > 512 {
		return fmt.Errorf("retained flow closure budget exceeded")
	}
	for k, list := range closures {
		if k == "" || len(list) == 0 || len(list) > 64 {
			return fmt.Errorf("retained flow closure entry %q is invalid", k)
		}
		for _, digest := range list {
			hexPart, ok := strings.CutPrefix(digest, "sha256-v1:")
			if !ok || !platform.ValidDigest("sha256:"+hexPart) {
				return fmt.Errorf("retained flow closure digest %q is invalid", digest)
			}
		}
	}
	return nil
}
