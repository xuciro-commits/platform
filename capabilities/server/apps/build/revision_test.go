package build

import (
	"encoding/json"
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
				{Ref: obj, ContractVersion: 1, SourceVersion: "1", Body: json.RawMessage(`{"type":"` + tc.object + `"}`)},
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
