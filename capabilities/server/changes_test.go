package platformserver

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChangesDistinguishesQueueProgressAndData(t *testing.T) {
	tenant := &Tenant{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { followChanges(w, r, tenant) }))
	defer server.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	read := func(want string) {
		t.Helper()
		var event strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if line == "\n" {
				break
			}
			event.WriteString(line)
		}
		if event.String() != want {
			t.Fatalf("event = %q, want %q", event.String(), want)
		}
	}
	read("event: changed\ndata: 0\n")
	tenant.operationsChanged()
	read("event: operations\ndata: 1\n")
	tenant.operationsChanged()
	tenant.changed()
	read("event: changed\ndata: 3\n")
}
