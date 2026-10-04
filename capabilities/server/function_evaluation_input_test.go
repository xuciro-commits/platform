package platformserver

import (
	"platformserver/platform"
	"testing"
)

func TestFunctionEvaluationMatchesLiveConversationPromptShape(t *testing.T) {
	f := platform.RecordAdviceFunction("build.note", []string{"name"}, []string{"user"})
	f.Output = []platform.Field{{Name: "summary", Type: "string", Required: true, Description: "Summary"}}
	if !functionEvaluationInput(f, []byte(`{"name":"Synthetic record"}`)) {
		t.Fatal("legacy flat evaluation rejected")
	}
	f.Conversation = true
	valid := `{"record":{"name":"Synthetic record"},"question":"Current synthetic question","history":[{"question":"Prior synthetic question","answer":{"summary":"Prior synthetic answer"}}]}`
	if !functionEvaluationInput(f, []byte(valid)) {
		t.Fatal("live-shaped synthetic conversation rejected")
	}
	for _, raw := range []string{`{"name":"Synthetic record"}`, `{"record":{"name":"Synthetic record"},"question":"","history":[]}`, `{"record":{"name":"Synthetic record"},"question":"Question","history":["CALL-1"]}`, `{"record":{"name":"Synthetic record"},"question":"Question","history":[{"question":"Prior","answer":{"unknown":"invalid"}}]}`, `{"record":{"name":"Synthetic record","secret":"extra"},"question":"Question","history":[]}`, `{"record":{"name":"Synthetic record"},"question":"Question","history":null}`} {
		if functionEvaluationInput(f, []byte(raw)) {
			t.Fatal("incompatible evaluation accepted", raw)
		}
	}
	f.MaxInputBytes = len(valid) - 1
	if functionEvaluationInput(f, []byte(valid)) {
		t.Fatal("evaluation escaped original byte budget")
	}
}
