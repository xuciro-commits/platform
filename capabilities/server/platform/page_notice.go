package platform

import (
	"fmt"
	"slices"
)

type PageNotice struct {
	Title   *string `json:"title,omitempty"`
	Message string  `json:"message"`
	Tone    string  `json:"tone"`
}

func (d *PageDocument) checkNotice(s Section) error {
	if s.Widget != "notice" {
		if s.Notice != nil {
			return fmt.Errorf("notice configuration needs its widget")
		}
		return nil
	}
	c := s.Notice
	limits := pageWidgets.Runtime.Notice
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || c == nil || c.Title != nil && len(*c.Title) > limits.MaxTitleBytes || len(c.Message) > limits.MaxMessageBytes || !slices.Contains(limits.Tones, c.Tone) || len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.Selection != "" {
		return fmt.Errorf("notice needs its profile, bounded plain text and declared tone without business bindings")
	}
	return nil
}
