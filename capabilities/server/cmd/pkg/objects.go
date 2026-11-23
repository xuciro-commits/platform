package main

import (
	"encoding/json"
	"fmt"
	"reflect"

	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/services/ontology"
	authoring "platformserver/services/resources/authoring"
)

func checkObjectActions(body *object) error {
	var declared ontology.Object
	if err := json.Unmarshal([]byte(stringify(body)), &declared); err != nil {
		return err
	}
	if err := ontology.CheckFields(declared.Fields, nil, declared.States...); err != nil {
		return err
	}
	if err := describes(declared); err != nil {
		return err
	}
	if refusal := ontology.CheckReferenceRecords(&declared); refusal != nil {
		return fmt.Errorf("%s: %s", declared.Type(), refusal.Message)
	}
	return ontology.CheckProcess(declared, authoring.ID, declared.Type(), authoring.Builder, authoring.User, protocol.FunctionCallType, nil)
}

func describes(declared ontology.Object) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("%v", failure)
		}
	}()
	_, err = records.Describe(authoring.ID, authoring.Entity(declared), func(reflect.Type) string { return "" })
	return err
}
