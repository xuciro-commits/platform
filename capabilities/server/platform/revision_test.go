package platform

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestReleaseCandidateCanonicalClosureAndDiff(t *testing.T) {
	object := AssetRef{App: "crm", Kind: AssetObject, Name: "crm.account"}
	action := AssetRef{App: "crm", Kind: AssetAction, Name: "crm.account.create"}
	page := AssetRef{App: "crm", Kind: AssetPage, Name: "accounts"}
	app := AssetRef{App: "crm", Kind: AssetApp, Name: "desk"}
	assets := []ReleaseAsset{
		{Ref: app, ContractVersion: 1, SourceVersion: "2", Requires: []AssetRef{page}, Body: json.RawMessage(`{"pages":["accounts"],"name":"desk","title":"CRM"}`)},
		{Ref: page, ContractVersion: 1, SourceVersion: "2", Requires: []AssetRef{action, object}, Body: json.RawMessage(`{"object":{"name":"crm.account","kind":"object","app":"crm"},"name":"accounts","actions":[{"app":"crm","kind":"action","name":"crm.account.create"}],"layout":"list-detail"}`)},
		{Ref: action, ContractVersion: 1, SourceVersion: "2", Requires: []AssetRef{object}, Body: json.RawMessage(`{"schema":"crm.account.create","target":"crm.account","title":"Create"}`)},
		{Ref: object, ContractVersion: 1, SourceVersion: "2", Body: json.RawMessage(`{"type":"crm.account","title":"Account","scope":{"owner":"member"}}`)},
	}
	first, err := Candidate([]AssetRef{app}, assets)
	if err != nil || len(first.Assets) != 4 || !strings.HasPrefix(first.ID, "sha256-v1:") {
		t.Fatalf("candidate: %+v %v", first, err)
	}
	if read, err := ReadCandidate(first.ID, first.Bytes); err != nil || read.ID != first.ID {
		t.Fatalf("saved candidate cannot be verified: %+v %v", read, err)
	}
	tampered := bytes.Replace(first.Bytes, []byte(`"owner":"member"`), []byte(`"owner":"vendor"`), 1)
	if _, err := ReadCandidate(first.ID, tampered); err == nil {
		t.Fatal("a changed release retained its old identity")
	}
	// Equivalent owner-produced descriptors and dependencies have one identity.
	assets[0].Body = json.RawMessage(`{ "title":"CRM", "name":"desk", "pages":["accounts"] }`)
	assets[1].Requires = []AssetRef{object, action}
	second, err := Candidate([]AssetRef{app}, assets)
	if err != nil || first.ID != second.ID || !bytes.Equal(first.Bytes, second.Bytes) {
		t.Fatalf("equivalent candidate changed: %v %v", first.ID, second.ID)
	}
	assets[3].Body = json.RawMessage(`{"type":"crm.account","title":"Account","scope":{"owner":"team"}}`)
	changed, err := Candidate([]AssetRef{app}, assets)
	if err != nil || changed.ID == first.ID {
		t.Fatalf("semantic change retained hash: %v", err)
	}
	add, remove, modify, err := CandidateDiff(first, changed)
	if err != nil || len(add) != 0 || len(remove) != 0 || len(modify) != 1 || modify[0] != object {
		t.Fatalf("diff: %v %v %v, %v", add, remove, modify, err)
	}
	// Callers cannot change the original canonical bytes by changing the draft.
	assets[3].Body[0] = ' '
	if !bytes.Contains(first.Bytes, []byte(`"owner":"member"`)) {
		t.Fatal("candidate aliases the draft bytes")
	}
}

func TestReleaseCandidateRefusesUnclosedAndAmbiguousAssets(t *testing.T) {
	object := AssetRef{App: "mes", Kind: AssetObject, Name: "mes.order"}
	page := AssetRef{App: "mes", Kind: AssetPage, Name: "orders"}
	valid := ReleaseAsset{Ref: object, ContractVersion: 1, SourceVersion: "1", Body: json.RawMessage(`{"type":"mes.order"}`)}
	cases := []struct {
		name   string
		roots  []AssetRef
		assets []ReleaseAsset
		want   string
	}{
		{"missing", []AssetRef{page}, []ReleaseAsset{{Ref: page, ContractVersion: 1, SourceVersion: "1", Requires: []AssetRef{object}, Body: json.RawMessage(`{"name":"orders","object":{"app":"mes","kind":"object","name":"mes.order"}}`)}}, "missing asset"},
		{"omitted page dependency", []AssetRef{page}, []ReleaseAsset{valid, {Ref: page, ContractVersion: 1, SourceVersion: "1", Body: json.RawMessage(`{"name":"orders","object":{"app":"mes","kind":"object","name":"mes.order"}}`)}}, "omits bound dependency"},
		{"omitted query dependency", []AssetRef{page}, []ReleaseAsset{valid, {Ref: page, ContractVersion: 1, SourceVersion: "1", Requires: []AssetRef{object}, Body: json.RawMessage(`{"name":"orders","object":{"app":"mes","kind":"object","name":"mes.order"},"sections":[{"widget":"table","query":{"app":"mes","kind":"query","name":"released-orders"}}]}`)}}, "omits bound dependency mes/query/released-orders"},
		{"cycle", []AssetRef{object}, []ReleaseAsset{{Ref: object, ContractVersion: 1, SourceVersion: "1", Requires: []AssetRef{object}, Body: json.RawMessage(`{"type":"mes.order"}`)}}, "cycle"},
		{"duplicate", []AssetRef{object}, []ReleaseAsset{valid, valid}, "declared twice"},
		{"duplicate JSON key", []AssetRef{object}, []ReleaseAsset{{Ref: object, ContractVersion: 1, SourceVersion: "1", Body: json.RawMessage(`{"type":"mes.order","scope":{"role":"a","role":"b"}}`)}}, "duplicate JSON key"},
		{"unversioned", []AssetRef{object}, []ReleaseAsset{{Ref: object, ContractVersion: 1, Body: json.RawMessage(`{"type":"mes.order"}`)}}, "no pinned"},
		{"wrong identity", []AssetRef{object}, []ReleaseAsset{{Ref: object, ContractVersion: 1, SourceVersion: "1", Body: json.RawMessage(`{"type":"mes.other"}`)}}, "identity differs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Candidate(tc.roots, tc.assets); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, err)
			}
		})
	}
}

func TestReleaseCandidateClosesReciprocalObjectReferences(t *testing.T) {
	location := AssetRef{App: "core", Kind: AssetObject, Name: "core.location"}
	for _, body := range []json.RawMessage{json.RawMessage(`{"type":"core.location","fields":[{"name":"parent","type":"reference","ref":"core.location"}]}`), json.RawMessage(`{"type":"core.location","entity":{"type":"core.location","fields":[{"name":"parent","type":"reference","ref":"core.location"}]}}`)} {
		candidate, err := Candidate([]AssetRef{location}, []ReleaseAsset{{Ref: location, ContractVersion: 1, SourceVersion: "1", Requires: []AssetRef{location}, Body: body}})
		if err != nil {
			t.Fatalf("declared parent reference was refused: %v", err)
		}
		if _, err := ReadCandidate(candidate.ID, candidate.Bytes); err != nil {
			t.Fatal(err)
		}
	}
	order := AssetRef{App: "mes", Kind: AssetObject, Name: "mes.order"}
	sfc := AssetRef{App: "mes", Kind: AssetObject, Name: "mes.sfc"}
	assets := []ReleaseAsset{
		{Ref: order, ContractVersion: 1, SourceVersion: "1", Requires: []AssetRef{sfc},
			Body: json.RawMessage(`{"type":"mes.order","fields":[{"name":"sfcs","ref":"mes.sfc"}]}`)},
		{Ref: sfc, ContractVersion: 1, SourceVersion: "1", Requires: []AssetRef{order},
			Body: json.RawMessage(`{"type":"mes.sfc","fields":[{"name":"order","ref":"mes.order"}]}`)},
	}
	first, err := Candidate([]AssetRef{order}, assets)
	if err != nil || len(first.Assets) != 2 {
		t.Fatalf("reciprocal relation is not closed: %v, %+v", err, first.Assets)
	}
	if _, err := ReadCandidate(first.ID, first.Bytes); err != nil {
		t.Fatal(err)
	}
	assets[1].Body = json.RawMessage(`{"type":"mes.sfc","fields":[{"name":"order","ref":"mes.order","required":true}]}`)
	changed, err := Candidate([]AssetRef{order}, assets)
	if err != nil || changed.ID == first.ID {
		t.Fatalf("referenced object rule did not change release: %v", err)
	}
	assets = assets[:1]
	if _, err := Candidate([]AssetRef{order}, assets); err == nil || !strings.Contains(err.Error(), "missing asset") {
		t.Fatalf("missing reciprocal target not diagnosed: %v", err)
	}
}

func TestReleaseCandidateNormalizesNumbersWithoutLosingPrecision(t *testing.T) {
	ref := AssetRef{App: "erp", Kind: AssetObject, Name: "erp.amount"}
	makeCandidate := func(body string) ReleaseCandidate {
		t.Helper()
		c, err := Candidate([]AssetRef{ref}, []ReleaseAsset{{Ref: ref, ContractVersion: 1,
			SourceVersion: "1", Body: json.RawMessage(body)}})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first := makeCandidate(`{"type":"erp.amount","value":12345678901234567890,"fraction":0.01}`)
	second := makeCandidate(`{"fraction":1e-2,"value":12345678901234567890.000,"type":"erp.amount"}`)
	if first.ID != second.ID || !bytes.Equal(first.Bytes, second.Bytes) {
		t.Fatal("equivalent precise numbers produced distinct revisions")
	}
	if !bytes.Contains(first.Bytes, []byte("12345678901234567890")) {
		t.Fatal("revision rounded a large integer")
	}
}
