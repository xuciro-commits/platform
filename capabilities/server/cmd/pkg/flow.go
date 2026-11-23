package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"platformserver/kernel/protocol"
	automationauthoring "platformserver/services/automate/authoring"
)

var flowBindingKeys = []string{"subject", "actions", "functions", "operations", "queries", "dependencies"}

func flowBindings(body *object, sourceVersion string) error {
	definition, _ := body.get("definition")
	var process protocol.Process
	decoder := json.NewDecoder(strings.NewReader(stringify(definition)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&process); err != nil {
		return fmt.Errorf("definition: %w", err)
	}
	asset, err := automationauthoring.FlowReleaseAsset("build", process, sourceVersion)
	if err != nil {
		return err
	}
	var computed map[string]json.RawMessage
	if err := json.Unmarshal(asset.Body, &computed); err != nil {
		return err
	}
	if want := computed["definition"]; !sameBinding(stringify(definition), string(want)) {
		value, err := parseJSON(want)
		if err != nil {
			return err
		}
		body.set("definition", value)
	}
	for _, key := range flowBindingKeys {
		want, has := computed[key]
		if got, declared := body.get(key); declared {
			if !sameBinding(stringify(got), string(want)) {
				shown := string(want)
				if !has || emptyBinding(want) {
					shown = "none"
				}
				return fmt.Errorf("%s is %s, but the steps give %s; leave %s out and pkg fills it in", key, stringify(got), shown, key)
			}
			continue
		}
		if !has || key != "actions" && emptyBinding(want) {
			continue
		}
		value, err := parseJSON(want)
		if err != nil {
			return err
		}
		body.set(key, value)
	}
	return nil
}

func emptyBinding(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	list, isList := value.([]any)
	return value == nil || isList && len(list) == 0
}

func sameBinding(got, want string) bool {
	if want == "" {
		return emptyBinding(json.RawMessage(got))
	}
	if emptyBinding(json.RawMessage(got)) && emptyBinding(json.RawMessage(want)) {
		return true
	}
	var a, b any
	return json.Unmarshal([]byte(got), &a) == nil && json.Unmarshal([]byte(want), &b) == nil && reflect.DeepEqual(a, b)
}
