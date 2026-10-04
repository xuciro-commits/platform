package platform

import (
	"fmt"
	"slices"
)

type PageLeaderboard struct {
	ValueField string `json:"valueField"`
	LabelField string `json:"labelField"`
	Limit      int    `json:"limit"`
	Ascending  bool   `json:"ascending"`
}

func (c PageLeaderboard) Sort() []string {
	field := c.ValueField
	if !c.Ascending {
		field = "-" + field
	}
	return []string{field, "id"}
}
func (d *PageDocument) checkLeaderboard(s Section) error {
	if s.Widget != "record-leaderboard" {
		if s.Leaderboard != nil {
			return fmt.Errorf("leaderboard configuration needs its widget")
		}
		return nil
	}
	if err := d.checkSharedRecordOutput(s); err != nil {
		return err
	}
	c, v := s.Leaderboard, d.Variables[s.CollectionVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Leaderboard.RequiredUIProfile) || c == nil || c.Limit < 1 || c.Limit > pageWidgets.Runtime.Leaderboard.MaxRanks || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || len(s.Fields) > 0 || len(s.Actions) > 0 {
		return fmt.Errorf("leaderboard needs its bounded original ranking window")
	}
	q := d.Queries[v.Source.Query]
	if q.Offset != 0 || q.Limit != c.Limit || !slices.Equal(q.Sort, c.Sort()) {
		return fmt.Errorf("leaderboard query needs its numeric/ID sort, zero offset and matching limit")
	}
	return nil
}
func (s Section) CheckLeaderboard(info EntityInfo) error {
	if s.Widget != "record-leaderboard" {
		return nil
	}
	if s.Leaderboard == nil {
		return fmt.Errorf("leaderboard fields are missing")
	}
	c := s.Leaderboard
	f, ok := info.Field(c.ValueField)
	label, labelOK := info.Field(c.LabelField)
	if !ok || !slices.Contains([]string{"integer", "decimal"}, f.Type) || c.LabelField != "id" && (!labelOK || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, label.Type)) {
		return fmt.Errorf("leaderboard needs original visible numeric and title fields")
	}
	return nil
}
func (p Page) CheckLeaderboardQuery(id string, named *Definition) error {
	if p.Document == nil {
		return nil
	}
	for _, s := range p.Sections {
		v := p.Document.Variables[s.CollectionVariable]
		if s.Widget == "record-leaderboard" && v.Source != nil && v.Source.Query == id && named != nil && named.Query != nil && len(named.Query.Sort) > 0 && (s.Leaderboard == nil || !slices.Equal(named.Query.Sort, s.Leaderboard.Sort())) {
			return fmt.Errorf("named query owns an incompatible leaderboard ordering")
		}
	}
	return nil
}
