package platform

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

func OwnedRelationType(app string) string { return app + ".__relation" }

// RelationEntity belongs to the application's own ledger. Hidden rows can only
// be read through typed traversal, which checks both endpoint grants.
func RelationEntity(app string) Entity {
	fields := []reflect.StructField{{Name: "Record", Type: reflect.TypeFor[Record](), Anonymous: true, Tag: reflect.StructTag(fmt.Sprintf(`entity:"%s"`, OwnedRelationType(app)))}}
	for _, field := range []string{"binding", "version", "parent", "child"} {
		fields = append(fields, reflect.StructField{Name: strings.ToUpper(field[:1]) + field[1:], Type: reflect.TypeFor[string](), Tag: reflect.StructTag(fmt.Sprintf(`json:"%s" field:"readonly"`, field))})
	}
	model := reflect.New(reflect.StructOf(fields)).Elem().Interface()
	return Entity{Type: OwnedRelationType(app), Title: "Owned relationship", Description: "An immutable typed relation instance of this application's authority", Model: model, Scope: Scope{Default: ScopeNone}}
}

type TxnLink struct {
	Binding AssetBinding `json:"binding"`
	Parent  TxnRef       `json:"parent"`
	Child   TxnRef       `json:"child"`
}

type OwnedRelationRuntime interface {
	PutOwnedRelation(Caller, *pb.ChangeRecord, TxnLink, bool, time.Time) *kernel.Error
}

func (c Caller) PutOwnedRelation(receipt *pb.ChangeRecord, link TxnLink, remove bool, at time.Time) *kernel.Error {
	runtime, ok := c.rt.(OwnedRelationRuntime)
	if !ok {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Owned relation writes require a private host")
	}
	return runtime.PutOwnedRelation(c, receipt, link, remove, at)
}

func RelationValue(entity Entity, id, binding, version, parent, child string, archived bool) any {
	value := reflect.New(reflect.TypeOf(entity.Model))
	_ = json.Unmarshal(Raw(map[string]any{"id": id, "binding": binding, "version": version, "parent": parent, "child": child, "archived": archived}), value.Interface())
	return value.Elem().Interface()
}
