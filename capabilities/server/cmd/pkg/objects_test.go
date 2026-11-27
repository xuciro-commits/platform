package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestReservedObjectActions(t *testing.T) {
	for _, name := range []string{"create", "edit", "archive", "restore", "reverse", "publish"} {
		t.Run(name, func(t *testing.T) {
			body, err := parseJSON([]byte(fmt.Sprintf(`{"namespace":"sample","name":"entry","title":"Entry","actions":[{"name":%q,"title":"Reserved","transaction":{}}]}`, name)))
			if err != nil {
				t.Fatal(err)
			}
			err = checkObjectActions(asObject(body))
			if err == nil || !strings.Contains(err.Error(), "sample.entry") || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "rename") {
				t.Fatalf("reserved action diagnostic: %v", err)
			}
		})
	}
}
