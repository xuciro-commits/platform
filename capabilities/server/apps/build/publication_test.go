package build

import (
	"strings"
	"testing"
)

func TestPublishedImageKeepsOnlyTheCurrentDefinition(t *testing.T) {
	for _, initial := range []struct {
		name string
		next func(string) string
	}{
		{"object", func(previous string) string {
			return published(Object{Name: "visit", Published: previous})
		}},
		{"page", func(previous string) string {
			return published(Page{Name: "visits", Published: previous})
		}},
		{"application", func(previous string) string {
			return published(Application{Name: "frontdesk", Published: previous})
		}},
	} {
		t.Run(initial.name, func(t *testing.T) {
			var previous string
			for range 20 {
				previous = initial.next(previous)
				if strings.Contains(previous, `"published"`) || len(previous) > 256 {
					t.Fatalf("published image recursively retained old versions (%d bytes)", len(previous))
				}
			}
		})
	}
}
