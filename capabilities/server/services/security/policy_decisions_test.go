package security

import (
	"encoding/json"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type stubCatalog struct{}

func (stubCatalog) Apps() []platform.App              { return nil }
func (stubCatalog) App(string) platform.App           { return nil }
func (stubCatalog) AppIDs() []string                  { return nil }
func (stubCatalog) Manifest(string) platform.Manifest { return platform.Manifest{} }

func projectSaveSubmission(t *testing.T, id string, members []platform.ProjectMember) *pb.Submission {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"title": "Project", "members": members})
	if err != nil {
		t.Fatal(err)
	}
	return &pb.Submission{Target: &pb.EntityRef{Type: platform.ProjectType, Id: id}, Schema: &pb.SchemaRef{Name: platform.SchemaProjectSave, Version: 1}, Payload: payload}
}

func TestPrepareProjectByOwnerDoesNotDeadlock(t *testing.T) {
	d := NewPolicies()
	d.Attach(stubCatalog{})
	d.projects["P1"] = &platform.BuildProject{Members: []platform.ProjectMember{{Member: "owner", Role: platform.ProjectOwnerRole}}}
	d.projects["P1"].ID = "P1"
	caller := platform.Caller{Member: platform.Member{ID: "owner"}}
	members := []platform.ProjectMember{{Member: "owner", Role: platform.ProjectOwnerRole}}
	done := make(chan struct{})
	var apply func(*pb.ChangeRecord)
	var kerr error
	go func() {
		a, e := d.PrepareProject(caller, projectSaveSubmission(t, "P1", members), []string{"owner"})
		apply = a
		if e != nil {
			kerr = e
		}
		close(done)
	}()
	<-done
	if kerr != nil {
		t.Fatalf("owner project save refused: %v", kerr)
	}
	if apply == nil {
		t.Fatal("owner project save produced no change")
	}
}

func TestPrepareProjectRefusesNonManager(t *testing.T) {
	d := NewPolicies()
	d.Attach(stubCatalog{})
	d.projects["P1"] = &platform.BuildProject{Members: []platform.ProjectMember{{Member: "owner", Role: platform.ProjectOwnerRole}}}
	d.projects["P1"].ID = "P1"
	caller := platform.Caller{Member: platform.Member{ID: "stranger"}}
	members := []platform.ProjectMember{{Member: "owner", Role: platform.ProjectOwnerRole}}
	apply, err := d.PrepareProject(caller, projectSaveSubmission(t, "P1", members), []string{"owner"})
	if err == nil || apply != nil {
		t.Fatal("non-manager must not save project access")
	}
}
