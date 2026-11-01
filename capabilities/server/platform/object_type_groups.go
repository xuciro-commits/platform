package platform

import (
	"fmt"
	"strings"
)

type ObjectTypeGroup struct {
	Name        string     `json:"name"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Members     []AssetRef `json:"members"`
}

func (g ObjectTypeGroup) Check() error {
	if !pageNodeID.MatchString(g.Name) || strings.TrimSpace(g.Title) == "" || len(g.Title) > 1024 || len(g.Description) > 16384 || len(g.Members) > 1000 {
		return fmt.Errorf("Object type group needs a valid name, title and at most 1000 members")
	}
	seen := map[AssetRef]bool{}
	for _, member := range g.Members {
		if member.Check() != nil || member.Kind != AssetObject || seen[member] {
			return fmt.Errorf("Object type group members must be distinct object references")
		}
		seen[member] = true
	}
	return nil
}
