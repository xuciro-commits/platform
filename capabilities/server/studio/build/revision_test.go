package build

import (
	"encoding/json"
	"reflect"
	"testing"

	"platformserver/platform"
)

func TestCodeAndBuilderPageCandidatesShareDescriptor(t *testing.T) {
	for _, tc := range []struct{ app, object, action, page string }{
		{"crm", "crm.account", "crm.account.create", "accounts"},
		{"mes", "mes.order", "mes.order.create", "orders"},
	} {
		t.Run(tc.app, func(t *testing.T) {
			obj := platform.AssetRef{App: tc.app, Kind: platform.AssetObject, Name: tc.object}
			act := platform.AssetRef{App: tc.app, Kind: platform.AssetAction, Name: tc.action}
			ref := platform.AssetRef{App: tc.app, Kind: platform.AssetPage, Name: tc.page}
			builderPage := descriptor(Page{Name: tc.page, Title: "Orders", Object: tc.object, List: []string{"name"}, Detail: []string{"name"}, Actions: []string{tc.action}})
			codePage := platform.Page{Name: tc.page, Title: "Orders", Object: obj, Layout: "list-detail", ListFields: []string{"name"}, DetailFields: []string{"name"}, Actions: []platform.AssetRef{act}}
			fromBuilder, err := platform.PageReleaseAsset(tc.app, "1", builderPage)
			if err != nil {
				t.Fatal(err)
			}
			fromCode, err := platform.PageReleaseAsset(tc.app, "1", codePage)
			if err != nil {
				t.Fatal(err)
			}
			available := []platform.ReleaseAsset{
				{Ref: obj, ContractVersion: 1, SourceVersion: "1", Body: json.RawMessage(`{"type":"` + tc.object + `","fields":[{"name":"name","type":"text"}]}`)},
				{Ref: act, ContractVersion: 1, SourceVersion: "1", Requires: []platform.AssetRef{obj}, Body: json.RawMessage(`{"schema":"` + tc.action + `","target":"` + tc.object + `"}`)},
				fromBuilder,
			}
			built, err := platform.Candidate([]platform.AssetRef{ref}, available)
			if err != nil {
				t.Fatal(err)
			}
			available[2] = fromCode
			coded, err := platform.Candidate([]platform.AssetRef{ref}, available)
			if err != nil || built.ID != coded.ID {
				t.Fatalf("code and builder diverged: %s %s, %v", built.ID, coded.ID, err)
			}
		})
	}
}

// Retained declarations keep their flat JSON, including fields whose visual
// schema now belongs to Web. No current visual type is needed to recover them.
func TestPageRetainedConfigurationRoundTripsFlatJSON(t *testing.T) {
	raw := []byte(`{"collectionOutputVariable":"CollectionOutputVariable","sceneSampleCollectionVariable":"SceneSampleCollectionVariable","sceneSampleVariable":"SceneSampleVariable","scenePartVariable":"ScenePartVariable","observationHistoryVariable":"ObservationHistoryVariable","observationContextVariable":"ObservationContextVariable","observationSignalVariable":"ObservationSignalVariable","observationThresholdVariable":"ObservationThresholdVariable","observationRowsVariable":"ObservationRowsVariable","observationCountVariable":"ObservationCountVariable","observationMeanVariable":"ObservationMeanVariable","notepadVariable":"NotepadVariable","analysisXVariable":"AnalysisXVariable","analysisYVariable":"AnalysisYVariable","analysisCountVariable":"AnalysisCountVariable","analysisMeanVariable":"AnalysisMeanVariable","historyLimit":7,"commentDraftVariable":"CommentDraftVariable","fileVariable":"FileVariable","pdfPageVariable":"PdfPageVariable","recordSetVariable":"RecordSetVariable","sparklineDecimalVariable":"SparklineDecimalVariable","sparklineNumberVariable":"SparklineNumberVariable","groupValueVariable":"GroupValueVariable","groupSetVariable":"GroupSetVariable","rowValueVariable":"RowValueVariable","rowSetVariable":"RowSetVariable","columnValueVariable":"ColumnValueVariable","columnSetVariable":"ColumnSetVariable","inputKind":"InputKind","pickerValueVariable":"PickerValueVariable","alertValueVariable":"AlertValueVariable","dateKind":"DateKind","dateOffset":"DateOffset","dateVariable":"DateVariable","choiceSetVariable":"ChoiceSetVariable","choiceVariable":"ChoiceVariable","booleanVariable":"BooleanVariable","summaryField":"SummaryField","statisticsVariable":"StatisticsVariable","gaugeValueVariable":"GaugeValueVariable","progressValueVariable":"ProgressValueVariable","progressTotalVariable":"ProgressTotalVariable","progressTotal":"ProgressTotal","countVariable":"CountVariable","selectionSetVariable":"SelectionSetVariable","widget":"table","id":"table","configVersion":1,"model":{"type":"build.note"},"actionDefaults":[{"parameter":"p","field":"name"}]}`)
	var section Section
	if err := json.Unmarshal(raw, &section); err != nil {
		t.Fatal(err)
	}
	page := Page{Name: "widgets", Object: "build.note", Sections: []Section{section}}
	installed := descriptor(page)
	restored, err := pageDraft(installed)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{section, installed.Sections[0], restored.Sections[0]} {
		var got, want map[string]json.RawMessage
		if err := json.Unmarshal(platform.Raw(value), &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		for name, original := range want {
			if !reflect.DeepEqual(got[name], original) {
				t.Fatalf("retained flat field %s changed: %s => %s", name, original, got[name])
			}
		}
	}
}

func TestPageSectionActionsKeepInheritanceAndExplicitRestrictions(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want []string
	}{
		{"inherit", `{"widget":"table"}`, nil},
		{"deny", `{"widget":"table","actions":[]}`, []string{}},
		{"narrow", `{"widget":"table","actions":["build.note.edit"]}`, []string{"build.note.edit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var section Section
			if err := json.Unmarshal([]byte(tc.raw), &section); err != nil {
				t.Fatal(err)
			}
			assertActions := func(got []string) {
				t.Helper()
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("section restriction changed: %#v, want %#v", got, tc.want)
				}
			}
			assertActions(section.Actions)
			var saved Section
			if err := json.Unmarshal(platform.Raw(section), &saved); err != nil {
				t.Fatal(err)
			}
			assertActions(saved.Actions)
			page := descriptor(Page{Name: "notes", Object: "build.note", Actions: []string{"build.note.edit"}, Sections: []Section{saved}})
			var read platform.Page
			if err := json.Unmarshal(platform.Raw(page), &read); err != nil {
				t.Fatal(err)
			}
			if (read.Sections[0].Actions == nil) != (tc.want == nil) || len(read.Sections[0].Actions) != len(tc.want) {
				t.Fatalf("published descriptor lost the original restriction: %#v", read.Sections[0].Actions)
			}
			draft, err := pageDraft(read)
			if err != nil {
				t.Fatal(err)
			}
			assertActions(draft.Sections[0].Actions)
		})
	}
}
