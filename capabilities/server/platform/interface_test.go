package platform

import (
	"strings"
	"testing"
)

type codedThing struct {
	Record
	Code string `json:"code" field:"required,search"`
	Name string `json:"name" field:"required,search"`
}

type numberedThing struct {
	Record
	Code int    `json:"code"`
	Name string `json:"name"`
}

func TestCheckInterfaces(t *testing.T) {
	coded := Interface{Name: "a.coded", Title: "Coded", Description: "Code and name.", Fields: []InterfaceField{{Name: "code", Type: "text"}, {Name: "name", Type: "text"}}}
	describe := func(app string, e Entity) EntityInfo {
		info, err := Describe(app, e, nil)
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	good := Entity{Type: "b.thing", Title: "Thing", Model: codedThing{}, Implements: []string{"a.coded"}}
	bad := Entity{Type: "b.numbered", Title: "Numbered", Model: numberedThing{}, Implements: []string{"a.coded"}}
	infos := map[string]EntityInfo{"b.thing": describe("b", good), "b.numbered": describe("b", bad)}
	a := Manifest{ID: "a", Interfaces: []Interface{coded}}
	if err := CheckInterfaces([]Manifest{a, {ID: "b", Entities: []Entity{good}}}, infos); err != nil {
		t.Fatalf("a conforming entity was refused: %v", err)
	}
	err := CheckInterfaces([]Manifest{a, {ID: "b", Entities: []Entity{bad}}}, infos)
	if err == nil || !strings.Contains(err.Error(), "is integer, not text") {
		t.Fatalf("a field of the wrong kind was not named: %v", err)
	}
	err = CheckInterfaces([]Manifest{{ID: "b", Entities: []Entity{good}}}, infos)
	if err == nil || !strings.Contains(err.Error(), "no app declares") {
		t.Fatalf("an undeclared interface was not refused: %v", err)
	}
	if err := CheckInterfaces([]Manifest{a, {ID: "b", Interfaces: []Interface{{Name: "a.coded", Title: "x", Description: "y", Fields: coded.Fields}}}}, infos); err == nil || !strings.Contains(err.Error(), "not <b>.<name>") {
		t.Fatalf("an interface outside its app's namespace was accepted: %v", err)
	}
}
