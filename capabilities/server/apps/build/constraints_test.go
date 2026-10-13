package build

import (
	"encoding/json"
	"slices"
	"testing"

	"platformserver/platform"
)

func TestConstraintPublicationAndFrozenObject(t *testing.T) {
	b := New("test")
	object := Object{Name: "entry", Title: "Entry", Fields: []Field{{Name: "qty", Title: "Quantity", Type: "integer"}}, Constraints: []platform.Constraint{{Name: "positive", Kind: "expression", Fields: []string{"qty"}, Message: "Positive quantities only.", Expr: &platform.Expr{Op: ">", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("integer", "qty"), platform.ExprLiteralValue("integer", 0)}}}}}
	if err := b.checkConstraints(object); err != nil {
		t.Fatal(err)
	}
	object.ID = "ENTRY"
	object.Published = published(object)
	assets, err := releaseAssets([]Object{object}, nil, nil, nil, "1")
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(assets, func(asset platform.ReleaseAsset) bool { return asset.Ref.Kind == platform.AssetObject })
	if index < 0 {
		t.Fatal("constraint has no owning object artifact")
	}
	var saved struct {
		Constraints []platform.Constraint `json:"constraints"`
	}
	if err := json.Unmarshal(assets[index].Body, &saved); err != nil || len(saved.Constraints) != 1 || saved.Constraints[0].Name != "positive" {
		t.Fatal("candidate dropped invariant", err)
	}
	object.Constraints[0].Message = "Changed draft"
	if string(assets[index].Body) == "" || saved.Constraints[0].Message == object.Constraints[0].Message {
		t.Fatal("draft changed frozen invariant")
	}
	object.Constraints[0].Kind = "script"
	if err := b.checkConstraints(object); err == nil {
		t.Fatal("builder accepted an unknown constraint")
	}
}
