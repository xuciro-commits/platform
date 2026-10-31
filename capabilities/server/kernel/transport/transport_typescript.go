package transport

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"platformserver/platform"
	"regexp"
	"slices"
	"strings"
)

func TypeScript(doc map[string]any, kernelModules map[string]string) string {
	defs := doc["components"].(map[string]any)["schemas"].(map[string]any)
	var names []string
	for name := range defs {
		if !strings.Contains(name, ":") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var b strings.Builder
	imports := map[string][]string{}
	for _, name := range names {
		if d, _ := defs[name].(map[string]any); d != nil && d["x-protobuf"] != nil {
			if module := kernelModules[name]; module != "" {
				imports[module] = append(imports[module], name+"Json")
			}
		}
	}
	var modules []string
	for m := range imports {
		modules = append(modules, m)
	}
	slices.Sort(modules)
	for _, m := range modules {
		slices.Sort(imports[m])
		fmt.Fprintf(&b, "import type { %s } from %q;\n", strings.Join(imports[m], ", "), m)
	}
	for _, name := range names {
		d, _ := defs[name].(map[string]any)
		if d["x-protobuf"] != nil {
			if kernelModules[name] != "" {
				fmt.Fprintf(&b, "\nexport type %s = %sJson;\n", name, name)
			} else {
				fmt.Fprintf(&b, "\nexport type %s = Record<string, unknown>;\n", name)
			}
			continue
		}
		fmt.Fprintf(&b, "\nexport type %s = %s;\n", name, tsOf(d, 0))
	}
	limits, _ := json.Marshal(map[string]int{"maxRegions": platform.MaxImageRegions, "maxLabelBytes": platform.MaxImageRegionLabelBytes, "maxIDBytes": platform.MaxImageRegionIDBytes})
	fmt.Fprintf(&b, "\nexport const imageRegionLimits = %s as const;\n", limits)
	return b.String()
}

func tsOf(s map[string]any, depth int) string {
	if ref, ok := s["$ref"].(string); ok {
		return strings.TrimPrefix(ref, "#/components/schemas/")
	}
	if enum, ok := s["enum"].([]string); ok {
		var parts []string
		for _, e := range enum {
			parts = append(parts, fmt.Sprintf("%q", e))
		}
		return strings.Join(parts, " | ")
	}
	switch s["type"] {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		items, _ := s["items"].(map[string]any)
		inner := tsOf(items, depth)
		if strings.Contains(inner, " | ") {
			inner = "(" + inner + ")"
		}
		return inner + "[]"
	case "object":
		if extra, ok := s["additionalProperties"].(map[string]any); ok {
			return "Record<string, " + tsOf(extra, depth) + ">"
		}
		props, _ := s["properties"].(map[string]any)
		order, _ := s["x-order"].([]string)
		required, _ := s["required"].([]string)
		indent := strings.Repeat("  ", depth+1)
		var b strings.Builder
		b.WriteString("{\n")
		for _, name := range order {
			opt := "?"
			if slices.Contains(required, name) {
				opt = ""
			}
			fmt.Fprintf(&b, "%s%s%s: %s;\n", indent, tsKey(name), opt, tsOf(props[name].(map[string]any), depth+1))
		}
		b.WriteString(strings.Repeat("  ", depth) + "}")
		return b.String()
	}
	return "unknown"
}

func tsKey(name string) string {
	if regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`).MatchString(name) {
		return name
	}
	return fmt.Sprintf("%q", name)
}

func KernelModules(dir string) map[string]string {
	out := map[string]string{}
	exported := regexp.MustCompile(`(?m)^export type (\w+)Json =`)
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_pb.ts") {
			return nil
		}
		raw, _ := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, strings.TrimSuffix(path, ".ts"))
		for _, m := range exported.FindAllStringSubmatch(string(raw), -1) {
			out[m[1]] = "./" + filepath.ToSlash(rel)
		}
		return nil
	})
	return out
}
