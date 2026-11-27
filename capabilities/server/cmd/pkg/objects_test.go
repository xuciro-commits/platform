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

func TestAskObjectActions(t *testing.T) {
	valid, err := parseJSON([]byte(`{
		"namespace":"sample","name":"lead","title":"Lead",
		"fields":[{"name":"title","title":"Title","type":"text","required":true}],
		"states":[{"name":"open","title":"Open"}],
		"access":[{"role":"manager","read":"all","create":true,"edit":true,"archive":true}],
		"ask":{"archive":"Removes from active lists."},
		"actions":[
			{"name":"dial","title":"Call","from":["open"],"ask":"Places an outbound call.","transaction":{"steps":[{"name":"s","kind":"update","target":{"type":"sample.lead","id":"$subject"},"changes":[{"field":"title","expr":{"kind":"text","lit":{"value":"called"}}}]}]}},
			{"name":"ping","title":"Ping","from":["open"],"ask":true,"transaction":{"steps":[{"name":"s","kind":"update","target":{"type":"sample.lead","id":"$subject"},"changes":[{"field":"title","expr":{"kind":"text","lit":{"value":"pinged"}}}]}]}}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkObjectActions(asObject(valid)); err != nil {
		t.Fatalf("valid ask rejected: %v", err)
	}

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"empty-reason", `{"namespace":"sample","name":"lead","title":"Lead","states":[{"name":"open","title":"Open"}],"actions":[{"name":"dial","title":"Call","from":["open"],"ask":"  ","transaction":{"steps":[]}}]}`, "ask"},
		{"bad-standard-key", `{"namespace":"sample","name":"lead","title":"Lead","access":[{"role":"manager","read":"all","archive":true}],"ask":{"delete":true}}`, "delete"},
		{"disabled-archive", `{"namespace":"sample","name":"lead","title":"Lead","access":[{"role":"manager","read":"all"}],"ask":{"archive":true}}`, "archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := parseJSON([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			err = checkObjectActions(asObject(body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}
