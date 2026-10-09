package mes

import (
	"google.golang.org/protobuf/types/known/durationpb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// DemoMaster is a small plant with two lines: pump housings on L1 and valve bodies
// on L2. It shows the three things the craft master data has to carry (ADR-0088):
// a routing is its own versioned thing that two products can share, an operation
// says what its equipment must be able to do and what it must hold while it runs,
// and a resource says what it can do — so "which equipment can process this step"
// is computed by matching, not typed in.
func DemoMaster() MasterData {
	return MasterData{
		Products: []Product{
			{ID: "P-100", Name: "Pump housing", Routing: "RT-100"},
			{ID: "P-150", Name: "Pump housing, bronze", Routing: "RT-100"}, // the same routing, reused
			{ID: "P-200", Name: "Valve body", Routing: "RT-200"},
		},
		Routings: []Routing{
			{ID: "RT-100", Name: "Pump housing routing", Version: 1, Operations: []Operation{
				{Number: 10, Name: "Cast", WorkCenter: "WC-CAST", Requires: []string{"casting"}, Parameters: []Parameter{
					{Name: "pour-temperature", Title: "Pour temperature", Type: "range", Unit: "°C", Target: 720, Min: 700, Max: 740},
					{Name: "mould-preheat", Title: "Mould preheat", Type: "setpoint", Unit: "°C", Target: 180}}},
				{Number: 20, Name: "Machine", WorkCenter: "WC-CNC-1", Requires: []string{"cnc-3axis"}, Parameters: []Parameter{
					{Name: "spindle-speed", Title: "Spindle speed", Type: "setpoint", Unit: "rpm", Target: 4200},
					{Name: "feed-rate", Title: "Feed rate", Type: "limit", Unit: "mm/min", Max: 900},
					{Name: "coolant", Title: "Coolant running", Type: "note"}}},
				{Number: 30, Name: "Inspect", WorkCenter: "WC-QC-1", Requires: []string{"cmm"}, Parameters: []Parameter{
					{Name: "bore-diameter", Title: "Bore diameter", Type: "range", Unit: "mm", Target: 48.02, Min: 48, Max: 48.04}}},
			}},
			// The valve body's routing as first released, and the revision in force
			// now: a deburr added after machining. A lot released against version 1
			// stays on version 1.
			{ID: "RT-200", Name: "Valve body routing", Version: 1, Operations: []Operation{
				{Number: 10, Name: "Machine", WorkCenter: "WC-CNC-2", Requires: []string{"cnc-3axis"}, Parameters: []Parameter{
					{Name: "spindle-speed", Title: "Spindle speed", Type: "setpoint", Unit: "rpm", Target: 3600}}},
				{Number: 20, Name: "Assemble", WorkCenter: "WC-ASM", Requires: []string{"assembly", "torque"}, Parameters: []Parameter{
					{Name: "seat-torque", Title: "Seat torque", Type: "range", Unit: "N·m", Target: 24, Min: 22, Max: 26}}},
				{Number: 30, Name: "Pressure test", WorkCenter: "WC-QC-2", Requires: []string{"pressure-test"}, Parameters: []Parameter{
					{Name: "test-pressure", Title: "Test pressure", Type: "setpoint", Unit: "bar", Target: 16},
					{Name: "hold-time", Title: "Hold time", Type: "setpoint", Unit: "s", Target: 30}}},
			}},
			{ID: "RT-200", Name: "Valve body routing", Version: 2, Operations: []Operation{
				{Number: 10, Name: "Machine", WorkCenter: "WC-CNC-2", Requires: []string{"cnc-3axis"}, Parameters: []Parameter{
					{Name: "spindle-speed", Title: "Spindle speed", Type: "setpoint", Unit: "rpm", Target: 3600}}},
				{Number: 20, Name: "Deburr", WorkCenter: "WC-CNC-2", Requires: []string{"cnc-3axis"}, Parameters: []Parameter{
					{Name: "deburr-feed", Title: "Deburr feed", Type: "limit", Unit: "mm/min", Max: 400}}},
				{Number: 30, Name: "Assemble", WorkCenter: "WC-ASM", Requires: []string{"assembly", "torque"}, Parameters: []Parameter{
					{Name: "seat-torque", Title: "Seat torque", Type: "range", Unit: "N·m", Target: 24, Min: 22, Max: 26}}},
				{Number: 40, Name: "Pressure test", WorkCenter: "WC-QC-2", Requires: []string{"pressure-test"}, Parameters: []Parameter{
					{Name: "test-pressure", Title: "Test pressure", Type: "setpoint", Unit: "bar", Target: 16},
					{Name: "hold-time", Title: "Hold time", Type: "setpoint", Unit: "s", Target: 30}}},
			}},
		},
		WorkCenters: []WorkCenter{
			{ID: "WC-CAST", Name: "Casting", Line: "L1"},
			{ID: "WC-CNC-1", Name: "Machining L1", Line: "L1"},
			{ID: "WC-QC-1", Name: "Inspection L1", Line: "L1"},
			{ID: "WC-CNC-2", Name: "Machining L2", Line: "L2"},
			{ID: "WC-ASM", Name: "Assembly", Line: "L2"},
			{ID: "WC-QC-2", Name: "Test L2", Line: "L2"},
		},
		// Two benches sit in Assembly; only ASM-1 holds the torque capability the
		// assemble operation asks for, so the computed answer is not "every resource
		// of the work center".
		Resources: []Resource{
			{ID: "FURNACE-1", Name: "Furnace 1", WorkCenter: "WC-CAST", Capabilities: []string{"casting"}},
			{ID: "CNC-11", Name: "CNC 11", WorkCenter: "WC-CNC-1", Capabilities: []string{"cnc-3axis", "cnc-5axis"}},
			{ID: "CNC-12", Name: "CNC 12", WorkCenter: "WC-CNC-1", Capabilities: []string{"cnc-3axis"}},
			{ID: "CMM-1", Name: "CMM 1", WorkCenter: "WC-QC-1", Capabilities: []string{"cmm"}},
			{ID: "CNC-21", Name: "CNC 21", WorkCenter: "WC-CNC-2", Capabilities: []string{"cnc-3axis"}},
			{ID: "ASM-1", Name: "Assembly bench 1", WorkCenter: "WC-ASM", Capabilities: []string{"assembly", "torque"}},
			{ID: "ASM-2", Name: "Assembly bench 2", WorkCenter: "WC-ASM", Capabilities: []string{"assembly"}},
			{ID: "TEST-1", Name: "Pressure test rig", WorkCenter: "WC-QC-2", Capabilities: []string{"pressure-test"}},
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
