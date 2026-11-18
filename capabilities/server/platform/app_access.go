package platform

import (
	"slices"
)

type CustomRole struct {
	ID      string   `json:"id"`
	App     string   `json:"app"`
	Title   string   `json:"title"`
	Actions []string `json:"actions"`
}

type PolicyRecord struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Effect     string            `json:"effect"`
	Permission string            `json:"permission"`
	Where      map[string]string `json:"where,omitempty"`
	From       string            `json:"from,omitempty"`
	Until      string            `json:"until,omitempty"`
}

type Team struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
	Grants  []Grant  `json:"grants"`
}

type AccessConfig struct {
	Roles    []CustomRole   `json:"roles"`
	Policies []PolicyRecord `json:"policies"`
	Teams    []Team         `json:"teams"`
	Markings []Marking      `json:"markings,omitempty"`
}

type ProjectMember struct {
	Member string `json:"member" field:"required" title:"Member" help:"A member's ID in this tenant"`
	Role   string `json:"role,omitempty" field:"required,choice:owner,editor,viewer,discoverer" title:"Role" help:"owner: edits the project's resources and manages its access; editor: edits its resources; viewer: opens them without editing"`
	Until  string `json:"until,omitempty" title:"Until" help:"Access ends before this day (YYYY-MM-DD); empty: no end"`
}

type BuildProject struct {
	Record
	Approvals int             `json:"approvals,omitempty"`
	Title     string          `json:"title" field:"required,search" title:"What people call it" example:"Spring rollout"`
	Members   []ProjectMember `json:"members,omitempty" title:"Members"`
}

type AuthorizationState struct {
	Roles    map[string]*CustomRole
	Policies map[string]*PolicyRecord
	Teams    map[string]*Team
	Projects map[string]*BuildProject
	Markings map[string]*Marking `json:",omitempty"`
}

var Markings = []string{"", "internal", "confidential", "restricted"}

func MarkingRank(m string) int { return max(slices.Index(Markings, m), 0) }

func MarkingCleared(m Member, marking string) bool {
	if MarkingRank(marking) <= MarkingRank("internal") {
		return true
	}
	for _, clearance := range Markings[MarkingRank(marking):] {
		if m.Holds("platform", clearance) || slices.Contains(m.Markings, clearance) {
			return true
		}
	}
	return false
}
