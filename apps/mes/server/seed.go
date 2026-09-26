package mes

import (
	"google.golang.org/protobuf/types/known/durationpb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// DemoMaster is a small plant with two lines: a pump housing on L1 and a valve body on L2.
func DemoMaster() MasterData {
	return MasterData{
		Products: []Product{
			{ID: "P-100", Name: "Pump housing", Routing: "RT-100", Operations: []Operation{
				{Step: 10, Name: "Cast", WorkCenter: "WC-CAST"}, {Step: 20, Name: "Machine", WorkCenter: "WC-CNC-1"},
				{Step: 30, Name: "Inspect", WorkCenter: "WC-QC-1"}}},
			{ID: "P-200", Name: "Valve body", Routing: "RT-200", Operations: []Operation{
				{Step: 10, Name: "Machine", WorkCenter: "WC-CNC-2"}, {Step: 20, Name: "Assemble", WorkCenter: "WC-ASM"},
				{Step: 30, Name: "Pressure test", WorkCenter: "WC-QC-2"}}},
		},
		WorkCenters: []WorkCenter{
			{ID: "WC-CAST", Name: "Casting", Line: "L1", Resources: []string{"FURNACE-1"}},
			{ID: "WC-CNC-1", Name: "Machining L1", Line: "L1", Resources: []string{"CNC-11", "CNC-12"}},
			{ID: "WC-QC-1", Name: "Inspection L1", Line: "L1", Resources: []string{"CMM-1"}},
			{ID: "WC-CNC-2", Name: "Machining L2", Line: "L2", Resources: []string{"CNC-21"}},
			{ID: "WC-ASM", Name: "Assembly", Line: "L2", Resources: []string{"ASM-1"}},
			{ID: "WC-QC-2", Name: "Test L2", Line: "L2", Resources: []string{"TEST-1"}},
		},
	}
}

// DemoOrganization is the demo plant in the site structure: the plant and its
// two lines, each line a unit whose ID is the line code the master data uses.
func DemoOrganization(members []platform.Membership) platform.OrgSeed {
	return platform.OrgSeed{
		Structures: []platform.Structure{{ID: SiteStructure, Name: "Sites", Kind: "site"}},
		Units: []platform.Unit{{ID: "plant-sz", Name: "Plant Shenzhen", Kind: "plant"},
			{ID: "L1", Name: "Line 1 · pump housings", Kind: "line"}, {ID: "L2", Name: "Line 2 · valve bodies", Kind: "line"}},
		Edges: []platform.Edge{{Structure: SiteStructure, Unit: "L1", Parent: "plant-sz", Relation: "part of"},
			{Structure: SiteStructure, Unit: "L2", Parent: "plant-sz", Relation: "part of"}},
		Memberships: members,
	}
}

// DemoConnectors: the line gateway pushes equipment states.
func DemoConnectors(tenant string) []*pb.ConnectorDescriptor {
	return []*pb.ConnectorDescriptor{
		{TenantId: tenant, ConnectorId: "gateway-l1", Direction: pb.ConnectorDirection_CONNECTOR_DIRECTION_PUSH,
			DataClasses: []string{ResourceType}, Heartbeat: durationpb.New(30e9)},
	}
}
