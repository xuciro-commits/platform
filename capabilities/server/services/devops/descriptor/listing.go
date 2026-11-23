package descriptor

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"platformserver/kernel/protocol"
)

var Industries = []string{
	"general", "manufacturing", "retail", "distribution", "hospitality", "healthcare", "education", "finance",
	"professional-services", "construction", "real-estate", "energy", "logistics", "agriculture", "nonprofit", "public-sector",
}

var Capabilities = []string{
	"sales", "marketing", "service", "hr", "payroll", "accounting", "inventory", "purchasing", "production",
	"projects", "assets", "maintenance", "quality", "learning", "knowledge", "lending", "operations", "master-data",
}

var Stages = []string{"stable", "preview", "test"}

var Relations = []string{"redesign", "derived", "original"}

func CheckListing(l protocol.PackageListing) error {
	for field, values := range map[string][]string{"industries": l.Industries, "capabilities": l.Capabilities} {
		allowed := Industries
		if field == "capabilities" {
			allowed = Capabilities
		}
		if len(values) > 8 {
			return fmt.Errorf("listing %s: at most 8", field)
		}
		for _, value := range values {
			if !slices.Contains(allowed, value) {
				return fmt.Errorf("listing %s: %q is not one of %s", field, value, strings.Join(allowed, ", "))
			}
		}
		if len(slices.Compact(slices.Sorted(slices.Values(values)))) != len(values) {
			return fmt.Errorf("listing %s: a value appears twice", field)
		}
	}
	if len(l.Keywords) > 10 {
		return fmt.Errorf("listing keywords: at most 10")
	}
	for _, keyword := range l.Keywords {
		if keyword == "" || len([]rune(keyword)) > 32 || strings.IndexFunc(keyword, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsUpper(r) }) >= 0 {
			return fmt.Errorf("listing keywords: %q must be 1–32 lowercase characters without spaces", keyword)
		}
	}
	if l.Stage != "" && !slices.Contains(Stages, l.Stage) {
		return fmt.Errorf("listing stage: %q is not one of %s", l.Stage, strings.Join(Stages, ", "))
	}
	if s := l.Source; s != nil {
		if s.Ecosystem == "" || s.App == "" || !slices.Contains(Relations, s.Relation) {
			return fmt.Errorf("listing source needs ecosystem, app and a relation of %s", strings.Join(Relations, ", "))
		}
	}
	return nil
}
