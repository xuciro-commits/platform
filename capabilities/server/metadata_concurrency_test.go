package platformserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// Discovery must see installed declarations at the same boundary as their
// publication, rather than iterating Build's caches during an installation.
func TestMetadataConcurrentPublication(t *testing.T) {
	tn, err := NewTenant("metadata", NewConsole("metadata", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}), build.New("metadata"))
	if err != nil {
		t.Fatal(err)
	}
	m, _ := tn.Member("builder")
	handler := NewHost(Tokens(map[string]string{"builder": "builder"}), tn).Handler()
	routes := []string{"/v1/actions", "/v1/apps", "/v1/me", "/v1/declarations", "/v1/protocols", "/v1/entities", "/v1/definitions", "/v1/capabilities", "/v1/capabilities/build/action/build.object.create", "/v1/openapi.json"}
	start := make(chan struct{})
	failures := make(chan error, len(routes)+1)
	var group sync.WaitGroup
	for _, path := range routes {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			for range 12 {
				r := httptest.NewRequest(http.MethodGet, path, nil)
				r.Header.Set("Authorization", "Bearer builder")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if w.Code != http.StatusOK {
					failures <- fmt.Errorf("%s: %d %s", path, w.Code, w.Body.String())
					return
				}
			}
		}()
	}
	group.Add(1)
	go func() {
		defer group.Done()
		<-start
		for i := range 12 {
			id := fmt.Sprintf("O%d", i)
			for _, step := range []struct {
				schema  string
				payload any
			}{{build.ObjectType + ".create", map[string]any{"name": fmt.Sprintf("task%d", i), "title": "Task", "fields": []build.Field{}}}, {build.SchemaPublish, struct{}{}}} {
				_, refusal := tn.Submit(m, &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: id + step.schema, Target: &pb.EntityRef{Type: build.ObjectType, Id: id}, Schema: &pb.SchemaRef{Name: step.schema, Version: 1}, Payload: platform.Raw(step.payload)}, time.Now())
				if refusal != nil {
					failures <- fmt.Errorf("publish %s: %s", id, refusal.Message)
					return
				}
			}
		}
	}()
	close(start)
	group.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if len(tn.Entities(m)) != 12+len(build.New("metadata").Manifest().Entities) {
		t.Fatal("discovery did not retain every published object")
	}
}
