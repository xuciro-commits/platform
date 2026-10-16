package platform

import (
	"fmt"
	"slices"
	"strings"
)

// AlertRule is the portable notification configuration, independent of its
// editable owner record and of notifications already delivered.
type AlertRule struct {
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Object   AssetRef `json:"object"`
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Value    string   `json:"value,omitempty"`
	Message  string   `json:"message"`
	Role     string   `json:"role,omitempty"`
	Every    bool     `json:"every,omitempty"`
	Active   bool     `json:"active"`
}

func (a AlertRule) Check() error {
	if a.Name == "" || strings.TrimSpace(a.Title) == "" || a.Object.Check() != nil || a.Object.Kind != AssetObject || a.Field == "" || strings.TrimSpace(a.Message) == "" || !slices.Contains([]string{"=", "!=", "<", "<=", ">", ">=", "empty", "not empty"}, a.Operator) {
		return fmt.Errorf("alert needs a name, title, object, field, operator and message")
	}
	return nil
}
