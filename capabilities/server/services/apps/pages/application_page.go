package pages

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

func ApplicationPage(a platform.Application) platform.Page {
	return platform.Page{
		Format:      4,
		InputSchema: platform.PageInputSchemaFor(a.UIProfile, a.Variables, a.Queries),
	}
}

func CheckApplicationProfile(p *platform.ApplicationProfile) error {
	if p == nil {
		return nil
	}
	if len(p.ExpectedRecords) > 1000 || p.ExpectedRuns < 0 || len(p.Retention) > 16 {
		return fmt.Errorf("application profile exceeds its declaration budget")
	}
	for typ, count := range p.ExpectedRecords {
		if count < 1 || !strings.Contains(typ, ".") || len(typ) > 256 {
			return fmt.Errorf("expected records need a type and a positive count")
		}
	}
	for name, duration := range p.Retention {
		if !slices.Contains([]string{"runs", "logs", "effects", "ai", "compute"}, name) {
			return fmt.Errorf("unknown application retention category")
		}
		if value, err := time.ParseDuration(duration); err != nil || value <= 0 {
			return fmt.Errorf("application retention needs a positive duration")
		}
	}
	for _, duration := range []string{p.RPO, p.RTO} {
		if duration == "" {
			continue
		}
		if value, err := time.ParseDuration(duration); err != nil || value <= 0 {
			return fmt.Errorf("application recovery declarations need positive durations")
		}
	}
	return nil
}
