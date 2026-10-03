package platform

import "fmt"

func (d *PageDocument) checkProgress(s Section) error {
	if s.Widget != "progress" {
		if s.ProgressLabel != "" || s.ProgressValueVariable != "" || s.ProgressTotalVariable != "" || s.ProgressTotal != "" {
			return fmt.Errorf("progress bindings need their widget")
		}
		return nil
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Progress.RequiredUIProfile) || s.ProgressValueVariable == "" || (s.ProgressTotalVariable != "") == (s.ProgressTotal != "") || len(s.ProgressLabel) > 1024 || len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.Selection != "" {
		return fmt.Errorf("progress needs its profile, numerator and exactly one denominator")
	}
	if s.ProgressTotal != "" {
		v, err := ParseDecimal(s.ProgressTotal)
		if err != nil || v.Value != s.ProgressTotal || v.Rat().Sign() <= 0 {
			return fmt.Errorf("fixed progress denominator needs a positive canonical decimal")
		}
	}
	return nil
}
