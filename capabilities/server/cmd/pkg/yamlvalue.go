package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

func toYAML(value any) *yaml.Node {
	switch v := value.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}
	case float64:
		if v == math.Trunc(v) && math.Abs(v) < 1<<53 {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(v), 10)}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(v, 'g', -1, 64)}
	case string:
		node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
		if strings.Contains(v, "\n") {
			node.Style = yaml.LiteralStyle
		}
		return node
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		if len(v) == 0 {
			node.Style = yaml.FlowStyle
		}
		for _, item := range v {
			node.Content = append(node.Content, toYAML(item))
		}
		return node
	case *object:
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		keys := v.order()
		if len(keys) == 0 {
			node.Style = yaml.FlowStyle
		}
		for _, key := range keys {
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, toYAML(v.values[key]))
		}
		return node
	}
	panic(fmt.Sprintf("cannot write %T as YAML", value))
}

func fromYAML(node *yaml.Node) (any, error) {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) != 1 {
			return nil, errors.New("a resource file holds exactly one document")
		}
		return fromYAML(node.Content[0])
	case yaml.AliasNode:
		return fromYAML(node.Alias)
	case yaml.MappingNode:
		out := &object{}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" {
				return nil, fmt.Errorf("line %d: keys are plain text", key.Line)
			}
			if _, repeated := out.get(key.Value); repeated {
				return nil, fmt.Errorf("line %d: %s appears twice", key.Line, key.Value)
			}
			value, err := fromYAML(node.Content[i+1])
			if err != nil {
				return nil, err
			}
			out.set(key.Value, value)
		}
		return out, nil
	case yaml.SequenceNode:
		out := []any{}
		for _, item := range node.Content {
			value, err := fromYAML(item)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	case yaml.ScalarNode:
		switch node.ShortTag() {
		case "!!str":
			return node.Value, nil
		case "!!null":
			return nil, nil
		case "!!bool":
			var b bool
			err := node.Decode(&b)
			return b, err
		case "!!int", "!!float":
			var f float64
			if err := node.Decode(&f); err != nil {
				return nil, fmt.Errorf("line %d: %w", node.Line, err)
			}
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, fmt.Errorf("line %d: numbers are finite", node.Line)
			}
			return f, nil
		}
		return nil, fmt.Errorf("line %d: %s values are not allowed", node.Line, node.ShortTag())
	}
	return nil, fmt.Errorf("line %d: unsupported YAML", node.Line)
}

func readYAML(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	value, err := fromYAML(&node)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return value, nil
}

func writeYAML(path string, value any) error {
	var out strings.Builder
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{toYAML(value)}}); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	return writeFile(path, out.String())
}
