package security

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

var markingName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func (d *Policies) decideMarking(c platform.Caller, s *pb.Submission, members []string) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	if !markingName.MatchString(id) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A marking ID uses lowercase letters, digits and hyphens")
	}
	prior := d.markings[id]
	next := platform.Marking{}
	if prior != nil {
		next = *prior
	}
	if s.GetSchema().GetName() == platform.SchemaMarkingArchive {
		if prior == nil || slices.Contains(platform.Markings, id) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Only a saved compartment marking can be archived")
		}
		next.Archived = true
	} else {
		var input struct {
			Title             string   `json:"title"`
			Members           []string `json:"members"`
			Teams             []string `json:"teams"`
			Declassifiers     []string `json:"declassifiers"`
			DeclassifierTeams []string `json:"declassifierTeams"`
		}
		if json.Unmarshal(s.GetPayload(), &input) != nil || strings.TrimSpace(input.Title) == "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A marking has a title")
		}
		for _, member := range append(slices.Clone(input.Members), input.Declassifiers...) {
			if !slices.Contains(members, member) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an existing member for marking permissions")
			}
		}
		for _, team := range append(slices.Clone(input.Teams), input.DeclassifierTeams...) {
			if d.teams[team] == nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an existing team for marking permissions")
			}
		}
		next.Title, next.Members, next.Teams = input.Title, slices.Clone(input.Members), slices.Clone(input.Teams)
		next.Declassifiers, next.DeclassifierTeams, next.Archived = slices.Clone(input.Declassifiers), slices.Clone(input.DeclassifierTeams), false
	}
	next.ID, next.RID = id, platform.ResourceID(c.Tenant, platform.AssetRef{App: "platform", Kind: "marking", Name: id})
	next.Classification = slices.Contains(platform.Markings, id)
	return func(r *pb.ChangeRecord) {
		stamp := platform.Stamp{By: c.ID, At: r.GetRecordedTime().AsTime(), Change: r.GetChangeId()}
		if next.Created.Change == "" {
			next.Created = stamp
		}
		next.Changed, next.Revision = stamp, r.GetRevision()
		d.markings[id] = &next
	}, nil
}

func (d *Policies) MarkingViews() []platform.Marking {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []platform.Marking{}
	for _, id := range slices.Sorted(maps.Keys(d.markings)) {
		out = append(out, *d.markings[id])
	}
	for _, id := range platform.Markings[1:] {
		if d.markings[id] == nil {
			out = append(out, platform.Marking{Record: platform.Record{ID: id}, Title: strings.ToUpper(id[:1]) + id[1:], Classification: true})
		}
	}
	slices.SortFunc(out, func(a, b platform.Marking) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func (d *Policies) MarkingClearances(member string) ([]string, []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var clearances, declassifications []string
	for id, marking := range d.markings {
		if marking.Archived {
			continue
		}
		allowed := func(members, teams []string) bool {
			if slices.Contains(members, member) {
				return true
			}
			return slices.ContainsFunc(teams, func(id string) bool { team := d.teams[id]; return team != nil && slices.Contains(team.Members, member) })
		}
		if allowed(marking.Members, marking.Teams) {
			clearances = append(clearances, id)
		}
		if allowed(marking.Declassifiers, marking.DeclassifierTeams) {
			declassifications = append(declassifications, id)
		}
	}
	slices.Sort(clearances)
	slices.Sort(declassifications)
	return clearances, declassifications
}
