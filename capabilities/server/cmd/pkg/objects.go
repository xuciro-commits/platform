package main

import (
	"encoding/json"

	"platformserver/kernel/protocol"
	"platformserver/services/ontology"
	authoring "platformserver/services/resources/authoring"
)

func checkObjectActions(body *object) error {
	var declared ontology.Object
	if err := json.Unmarshal([]byte(stringify(body)), &declared); err != nil {
		return err
	}
	return ontology.CheckProcess(declared, authoring.ID, declared.Type(), authoring.Builder, authoring.User, protocol.FunctionCallType, nil)
}
