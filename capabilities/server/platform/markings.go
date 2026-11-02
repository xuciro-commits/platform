package platform

import "slices"

const MarkingType = "platform.marking"
const SchemaMarkingSave = MarkingType + ".save"
const SchemaMarkingArchive = MarkingType + ".archive"

type Marking struct {
	Record
	RID               string   `json:"rid"`
	Title             string   `json:"title"`
	Classification    bool     `json:"classification"`
	Cleared           bool     `json:"cleared,omitempty"`
	CanDeclassify     bool     `json:"canDeclassify,omitempty"`
	Members           []string `json:"members,omitempty"`
	Teams             []string `json:"teams,omitempty"`
	Declassifiers     []string `json:"declassifiers,omitempty"`
	DeclassifierTeams []string `json:"declassifierTeams,omitempty"`
}

type MarkingSet struct {
	Level        string   `json:"level,omitempty"`
	Compartments []string `json:"compartments,omitempty"`
}

func JoinCompartments(groups ...[]string) []string {
	var all []string
	for _, group := range groups {
		all = append(all, group...)
	}
	slices.Sort(all)
	return slices.Compact(all)
}

func (s MarkingSet) Cleared(m Member) bool {
	return MarkingCleared(m, s.Level) && !slices.ContainsFunc(s.Compartments, func(id string) bool { return !slices.Contains(m.Markings, id) })
}

func (s MarkingSet) Without(markings []string) MarkingSet {
	if slices.Contains(markings, s.Level) {
		s.Level = "internal"
	}
	s.Compartments = slices.DeleteFunc(slices.Clone(s.Compartments), func(id string) bool { return slices.Contains(markings, id) })
	return s
}

func (s MarkingSet) Declassify(m Member, markings []string) (MarkingSet, bool) {
	if !s.Cleared(m) || !m.CanDeclassify(markings) {
		return s, false
	}
	return s.Without(markings), true
}

func (s MarkingSet) Guarded() bool {
	return MarkingRank(s.Level) >= MarkingRank("confidential") || len(s.Compartments) > 0
}

func (m Member) CanDeclassify(markings []string) bool {
	for _, id := range markings {
		cleared := slices.Contains(m.Markings, id)
		if slices.Contains(Markings[1:], id) {
			cleared = MarkingCleared(m, id)
		}
		if !cleared || !slices.Contains(m.Declassifications, id) {
			return false
		}
	}
	return true
}
