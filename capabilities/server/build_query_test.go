package platformserver

import (
	"encoding/json"

	"platformserver/platform"
	"testing"
)

func TestRetainedDefinitionVersionsPreserveHistoryAndCompareContent(t *testing.T) {
	first := platform.NamedQuery{Name: "shared", Title: "First query", Object: "build.note"}
	previous := first
	previous.Title = "Previous query"
	current := first
	current.Title = "Current query"
	history := map[string]platform.NamedQuery{"1.query-1": first}
	original := string(platform.Raw(history))
	retained, ok := retainedDefinitionVersions(history, "1.query-2", &previous, "1.query-3", current)
	if !ok || len(retained) != 2 || string(platform.Raw(retained["1.query-1"])) != string(platform.Raw(first)) || string(platform.Raw(retained["1.query-2"])) != string(platform.Raw(previous)) {
		t.Fatal("retained versions lost history or the previous current definition", retained, ok)
	}
	if _, exists := retained["1.query-3"]; exists {
		t.Fatal("version preparation wrote the new current definition before installation")
	}
	retained["1.query-1"] = current
	if string(platform.Raw(history)) != original {
		t.Fatal("version preparation modified the original history map")
	}
	if _, ok := retainedDefinitionVersions(history, "", nil, "1.query-1", current); ok {
		t.Fatal("a retained version accepted different content")
	}
	if _, ok := retainedDefinitionVersions(history, "", nil, "1.query-1", first); !ok {
		t.Fatal("a retained version rejected its original content")
	}
}
func queryRecordID(record any, id string) bool {
	raw, _ := json.Marshal(record)
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	return value["id"] == id
}
