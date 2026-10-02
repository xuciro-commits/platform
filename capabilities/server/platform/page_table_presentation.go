package platform

import (
	"fmt"
	"slices"
)

// Column presentation never replaces the object's field or edit contract.
type PageTableColumn struct {
	Field     string `json:"field"`
	Title     string `json:"title,omitempty"`
	Width     int    `json:"width,omitempty"`
	Formatter string `json:"formatter,omitempty"`
}

func (d *PageDocument) checkTablePresentation(s Section) error {
	if len(s.TableColumns) == 0 && s.ShowSearch == nil {
		return nil
	}
	limits := pageWidgets.Runtime.TablePresentation
	if s.Widget != "table" || !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || len(s.TableColumns) > limits.MaxColumns {
		return fmt.Errorf("table presentation needs its supported profile and bounded columns")
	}
	seen := map[string]bool{}
	for _, c := range s.TableColumns {
		if !pageNodeID.MatchString(c.Field) || seen[c.Field] || c.Field != "id" && !slices.Contains(s.Fields, c.Field) || len(c.Title) > limits.MaxTitleBytes || c.Width != 0 && (c.Width < limits.MinWidth || c.Width > limits.MaxWidth) {
			return fmt.Errorf("table column needs a unique displayed field and bounded title/width")
		}
		if c.Formatter != "" && !slices.ContainsFunc(limits.Formatters, func(f struct {
			ID         string   `json:"id"`
			FieldTypes []string `json:"fieldTypes"`
		}) bool {
			return f.ID == c.Formatter
		}) {
			return fmt.Errorf("unknown table column formatter")
		}
		if c.Field == "id" && c.Formatter != "" && c.Formatter != "text" {
			return fmt.Errorf("record identity only supports text presentation")
		}
		seen[c.Field] = true
	}
	return nil
}

func (s Section) CheckTablePresentation(info EntityInfo) error {
	for _, c := range s.TableColumns {
		if c.Field == "id" {
			continue
		}
		field, ok := info.Field(c.Field)
		if !ok || field.Aside || field.Type == "lines" {
			return fmt.Errorf("table column field is unavailable")
		}
		if c.Formatter == "" {
			continue
		}
		allowed := false
		for _, f := range pageWidgets.Runtime.TablePresentation.Formatters {
			if f.ID == c.Formatter && slices.Contains(f.FieldTypes, field.Type) {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("table formatter does not support its field type")
		}
	}
	return nil
}
