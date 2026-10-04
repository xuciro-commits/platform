package platform

import "testing"

func TestThreeAIViewsUseOriginalRecordFunctionAndOwnedQuestion(t *testing.T) {
	p := embeddedTestPage("ai")
	p.Sections = []Section{{ID: "table", Widget: "table", ConfigVersion: 1}, {ID: "ai", Widget: "ai-assistant", ConfigVersion: 1, RecordVariable: "record", Function: &AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetFunction, Name: "advice"}, SourceVersion: "1.function-1"}, AI: &PageAI{Kind: "analyst"}}}
	p.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table", "ai"}}, "table": {Kind: "widget", Section: "table"}, "ai": {Kind: "widget", Section: "ai"}}
	p.Document.Variables = map[string]PageVariable{"record": {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table"}}, "question": {Scope: "page", Type: "string", Mode: "state", Initial: Raw("")}}
	f := RecordAdviceFunction(p.Object.Name, []string{"name"}, []string{"reader"})
	for _, kind := range []string{"analyst", "generated", "chatbot"} {
		section := p.Sections[1]
		section.AI = &PageAI{Kind: kind}
		f.Conversation = kind == "chatbot"
		if kind == "chatbot" {
			section.AI.QuestionVariable = "question"
			section.AI.ReplyField = "summary"
			section.AI.Suggestions = []string{"Summarise this record"}
		}
		p.Sections[1] = section
		if err := p.Document.Check(p.Sections); err != nil {
			t.Fatal(kind, err)
		}
		if err := p.CheckAIBinding(section, f); err != nil {
			t.Fatal(kind, err)
		}
		f.Conversation = !f.Conversation
		if p.CheckAIBinding(section, f) == nil {
			t.Fatal("wrong conversation contract accepted", kind)
		}
	}
	var missing *PageDocument
	if missing.Check(p.Sections) == nil {
		t.Fatal("AI configuration accepted without a document")
	}
	p.Document.UIProfile = "platform.page.v2.85"
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("old AI renderer profile accepted")
	}
	p.Document.UIProfile = PageUIProfile()
	s := p.Sections[1]
	s.AI.QuestionVariable = "record"
	p.Sections[1] = s
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("record became writable question text")
	}
}
