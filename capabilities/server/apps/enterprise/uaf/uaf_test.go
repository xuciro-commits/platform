package uaf

import "testing"

func TestProfile(t *testing.T) {
	m := Current()
	if m.Version != "1.3" || len(m.Stereotypes) != 272 || len(m.Enumerations) != 21 {
		t.Fatalf("version %s, %d stereotypes, %d enumerations", m.Version, len(m.Stereotypes), len(m.Enumerations))
	}
	ao := m.Stereotypes["ActualOrganization"]
	if ao == nil || ao.Domain != "Actual Resources" || len(ao.Bases) != 1 || ao.Bases[0] != "InstanceSpecification" || ao.Relationship() {
		t.Fatalf("ActualOrganization %+v", ao)
	}
	if !m.Is("ActualOrganization", "ActualOrganizationalResource") || !m.Is("ActualPerson", "ActualResponsibleResource") {
		t.Fatal("generalisations not resolved")
	}
	if fp := m.Stereotypes["FillsPost"]; fp == nil || !fp.Relationship() {
		t.Fatalf("FillsPost %+v", fp)
	}
	cap := m.Stereotypes["Capability"]
	var kind *Property
	for i := range cap.Properties {
		if cap.Properties[i].Name == "kind" {
			kind = &cap.Properties[i]
		}
	}
	if kind == nil || kind.Type != "CapabilityKind" || len(m.Enumerations["CapabilityKind"].Literals) == 0 {
		t.Fatalf("Capability.kind %+v", kind)
	}
	if ap := m.Stereotypes["ActualProject"]; ap == nil || len(m.Properties("ActualProject")) < 2 {
		t.Fatalf("ActualProject properties %v", m.Properties("ActualProject"))
	}
}

func TestLineage(t *testing.T) {
	m := Current()
	if !m.Is("ActualOrganization", "ActualResource") || m.Is("ActualOrganization", "Capability") || m.Is("UAFElement", "ActualOrganization") {
		t.Fatal("Is does not follow generalisations")
	}
	if !m.Relationship("ActualResourceRelationship") || !m.Relationship("FillsPost") || m.Relationship("ActualOrganization") || m.Relationship("ActualOrganizationRole") {
		t.Fatal("Relationship does not read the UML base")
	}
}
