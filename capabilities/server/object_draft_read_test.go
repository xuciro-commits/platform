package platformserver

import "platformserver/internal/construction"

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"platformserver/platform"
)

func TestLegacyObjectDraftReadAndSavePreservesTransactionIntent(t *testing.T) {
	tn := builderTenant(t, "legacy-object-read")
	member := memberOf(t, tn, "builder")
	object := construction.Object{Name: "legacy", Title: "Legacy document", Fields: []construction.Field{{Name: "number", Title: "Number", Type: "text"}, {Name: "note", Title: "Note", Type: "text"}},
		States: []construction.State{{Name: "draft", Title: "Draft"}, {Name: "done", Title: "Done"}}, Numbering: &construction.Numbering{Field: "number", Prefix: "L", Width: 4},
		Actions: []construction.Action{{Name: "finish", Title: "Finish", From: []string{"draft"}, To: "done", Inputs: []construction.Input{{Name: "note", Title: "Note", Type: "text", Required: true}},
			Sets: []construction.Set{{Field: "note", From: "note"}}, Conditions: []construction.Condition{{Field: "input.note", Operator: "not empty", Message: "Enter a note"}}}}}
	var entries []Entry
	for _, step := range []struct {
		schema  string
		payload any
	}{{"build.object.create", map[string]any{"name": object.Name, "title": object.Title, "fields": object.Fields, "states": object.States, "numbering": object.Numbering, "actions": object.Actions}}, {"build.object.publish", map[string]any{}}} {
		input := submission(tn, member, construction.ID, step.schema, construction.ObjectType, "LEGACY", step.payload)
		raw, err := protojson.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, Entry{App: construction.ID, Kind: "submission", Principal: platform.Raw(member), Body: raw, At: txnAt})
	}
	if err := tn.Replay(entries); err != nil {
		t.Fatal(err)
	}
	held, _ := tn.Held(construction.ObjectType + "/LEGACY")
	legacy := held.(construction.Object)
	before := string(platform.Raw(legacy))
	if legacy.Actions[0].Transaction != nil || legacy.Published == "" {
		t.Fatal("historical setup did not retain its original action and publication")
	}

	server := httptest.NewServer(NewHost(Tokens(map[string]string{"builder": "builder", "user": "user"}), tn).Handler())
	defer server.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	read := func(path, token string) (int, json.RawMessage) {
		t.Helper()
		target, body, post := strings.Cut(path, "\n")
		method := http.MethodGet
		if post {
			method = http.MethodPost
		}
		request, err := http.NewRequest(method, server.URL+target, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var value json.RawMessage
		if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, value
	}
	path := "/v1/records/build.object/LEGACY"
	status, raw := read(path, "builder")
	var answer struct {
		Record      construction.Object `json:"record"`
		ValueErrors map[string]string   `json:"valueErrors"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil || status != http.StatusOK || len(answer.ValueErrors) != 0 {
		t.Fatalf("draft GET: HTTP %d, %s, %v", status, raw, err)
	}
	canonical := answer.Record
	if canonical.Published != legacy.Published || canonical.Numbering != nil || canonical.CreateTransaction == nil || canonical.Actions[0].Transaction == nil || canonical.Actions[0].Transaction.Guard == nil || len(canonical.Actions[0].Sets) != 0 || canonical.Actions[0].To != "" {
		t.Fatal("draft read lost legacy intent or changed its retained publication")
	}
	if held, _ := tn.Held(construction.ObjectType + "/LEGACY"); string(platform.Raw(held)) != before {
		t.Fatal("reading an editable projection changed the accepted record")
	}
	for _, path := range []string{"/v1/records/build.object", "/v1/records/build.object/query\n{}"} {
		status, raw := read(path, "builder")
		var page struct {
			Records []construction.Object `json:"records"`
		}
		if err := json.Unmarshal(raw, &page); err != nil || status != http.StatusOK || len(page.Records) != 1 || !reflect.DeepEqual(page.Records[0], canonical) {
			t.Fatalf("list/query did not use the canonical read: %s HTTP %d %v", path, status, err)
		}
	}
	userView, userRefusal := tn.RecordOf(memberOf(t, tn, "user"), construction.ObjectType, "LEGACY", txnAt)
	userStatus, userRaw := read(path, "user")
	if userRefusal != nil {
		if userStatus != http.StatusForbidden && userStatus != http.StatusNotFound {
			t.Fatal("authoring projection widened the original record permissions")
		}
	} else {
		var userAnswer struct {
			Record construction.Object `json:"record"`
		}
		if err := json.Unmarshal(userRaw, &userAnswer); err != nil || userStatus != http.StatusOK || string(platform.Raw(userAnswer.Record)) != string(platform.Raw(userView.Record)) {
			t.Fatal("non-authoring member received a different record projection")
		}
	}

	watch, _ := json.Marshal([]string{path})
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/changes?"+url.Values{"watch": {string(watch)}}.Encode(), nil)
	request.Header.Set("Authorization", "Bearer builder")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var frame LiveQueryFrame
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil || len(frame.Results) != 1 || frame.Results[0].Status != http.StatusOK {
			t.Fatalf("live draft read: %s %v", line, err)
		}
		var live struct {
			Record construction.Object `json:"record"`
		}
		if err := json.Unmarshal(frame.Results[0].Body, &live); err != nil || !reflect.DeepEqual(live.Record, canonical) {
			t.Fatal("shared live read differs from the original draft GET")
		}
		break
	}
	response.Body.Close()

	canonical.Actions[0].Title = "Finish with note"
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "LEGACY", map[string]any{"actions": canonical.Actions}, txnAt.Add(time.Minute))
	held, _ = tn.Held(construction.ObjectType + "/LEGACY")
	saved := held.(construction.Object)
	if saved.Actions[0].Title != "Finish with note" || !reflect.DeepEqual(saved.Actions[0].Transaction, canonical.Actions[0].Transaction) || saved.CreateTransaction == nil || saved.Numbering != nil || saved.Published != legacy.Published {
		t.Fatal("saving the editable draft discarded legacy behavior or rewrote published bytes")
	}
}
