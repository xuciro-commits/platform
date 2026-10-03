package platform

import "testing"

func TestLeaderboardOriginalTopNOrderingAndIdentity(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	qid := d.Variables["window"].Source.Query
	q := d.Queries[qid]
	q.Sort = []string{"-qty", "id"}
	q.Limit = 8
	d.Queries[qid] = q
	s := Section{ID: "rank", Widget: "record-leaderboard", ConfigVersion: 1, CollectionVariable: "window", Leaderboard: &PageLeaderboard{ValueField: "qty", LabelField: "title", Limit: 8}}
	p.Sections = append(p.Sections, s)
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	d.Variables["rankRecord"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: s.ID}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckLeaderboard(EntityInfo{Fields: []FieldInfo{{Name: "qty", Type: "decimal"}, {Name: "title", Type: "text"}}}); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.50"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted leaderboard")
	}
	d.UIProfile = PageUIProfile()
	q.Offset = 8
	d.Queries[qid] = q
	if d.checkLeaderboard(s) == nil {
		t.Fatal("partial page called top ranks")
	}
	q.Offset = 0
	q.Sort = []string{"-qty"}
	d.Queries[qid] = q
	if d.checkLeaderboard(s) == nil {
		t.Fatal("unstable tie ordering accepted")
	}
	q.Sort = []string{"-qty", "id"}
	d.Queries[qid] = q
	if s.CheckLeaderboard(EntityInfo{Fields: []FieldInfo{{Name: "qty", Type: "money"}, {Name: "title", Type: "text"}}}) == nil {
		t.Fatal("currency folded into ranking")
	}
	if s.CheckLeaderboard(EntityInfo{}) == nil {
		t.Fatal("hidden fields accepted")
	}
	named := &Definition{Query: &NamedQuery{Sort: []string{"id"}}}
	if p.CheckLeaderboardQuery(qid, named) == nil {
		t.Fatal("incompatible named order accepted")
	}
	named.Query.Sort = q.Sort
	if err := p.CheckLeaderboardQuery(qid, named); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("leaderboard without document accepted")
	}
}
