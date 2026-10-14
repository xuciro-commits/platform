package platform

import (
	"fmt"
	"strings"
)

// AppConcept is a builder-named business concept in the application studio.
// Its stable ID survives renames; its assets retain their original identity.
type AppConcept struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Assets []AssetRef `json:"assets"`
}

// CheckConcepts validates navigation metadata, independently of compilation.
// A historical group can still name a removed member: only current application
// members are drawn, so removing membership never requires rewriting old groups.
func (a Application) CheckConcepts() error {
	if len(a.Concepts) > 100 {
		return fmt.Errorf("application concepts exceed 100 groups")
	}
	ids, assets := map[string]bool{}, map[AssetRef]bool{}
	count := 0
	for _, concept := range a.Concepts {
		if strings.TrimSpace(concept.ID) == "" || len(concept.ID) > 128 || ids[concept.ID] {
			return fmt.Errorf("application concepts need unique nonempty IDs of at most 128 bytes")
		}
		ids[concept.ID] = true
		if strings.TrimSpace(concept.Title) == "" || len(concept.Title) > 512 {
			return fmt.Errorf("application concepts need a title of at most 512 bytes")
		}
		for _, ref := range concept.Assets {
			if err := ref.Check(); err != nil {
				return err
			}
			if ref.Kind == AssetApp || assets[ref] {
				return fmt.Errorf("concept assets must be unique resources, not application roots")
			}
			assets[ref] = true
			count++
		}
	}
	if count > 1000 {
		return fmt.Errorf("application concepts exceed 1000 grouped assets")
	}
	return nil
}
