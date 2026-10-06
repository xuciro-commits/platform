package platform

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type PageBreadcrumb struct {
	HomeLabel  string `json:"homeLabel"`
	PageLabel  string `json:"pageLabel"`
	LabelField string `json:"labelField,omitempty"`
}
type PageAvatarStack struct {
	LabelField                string   `json:"labelField"`
	DetailFields              []string `json:"detailFields,omitempty"`
	ContextVariable           string   `json:"contextVariable,omitempty"`
	ContextCollectionVariable string   `json:"contextCollectionVariable,omitempty"`
}
type PageStaticImage struct {
	URL     string   `json:"url,omitempty"`
	Caption *string  `json:"caption,omitempty"`
	Height  *float64 `json:"height,omitempty"`
}

func contextView(widget string) bool {
	return slices.Contains([]string{"breadcrumb", "avatar-stack", "static-image"}, widget)
}

// ContextViewVariables also drives projection and exact page/Overlay ownership.
func (s Section) ContextViewVariables() []string {
	ids := []string{}
	if s.Widget == "breadcrumb" {
		ids = append(ids, s.RecordVariable)
	}
	if s.Avatar != nil {
		ids = append(ids, s.CollectionVariable, s.Avatar.ContextVariable, s.Avatar.ContextCollectionVariable)
	}
	return ids
}
func (d *PageDocument) checkContextViews(s Section) error {
	if s.Breadcrumb != nil && s.Widget != "breadcrumb" || s.Avatar != nil && s.Widget != "avatar-stack" || s.Image != nil && s.Widget != "static-image" {
		return fmt.Errorf("context presentation configuration needs its original widget")
	}
	if !contextView(s.Widget) {
		return nil
	}
	c := pageWidgets.Runtime.ContextViews
	if !PageUIProfileSupports(d.UIProfile, c.RequiredUIProfile) || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Selection != "" || s.SelectionVariable != "" || s.SelectionSetVariable != "" || s.RecordSetVariable != "" || s.FilterVariable != "" || s.Relation != "" || s.ParentSelection != "" || s.Query != (AssetRef{}) || s.InlineEdit != nil || s.Function != nil || s.Operation != nil || len(s.Inputs) > 0 {
		return fmt.Errorf("context views need their finite profile without independent business bindings")
	}
	record := func(id string) bool {
		v := d.Variables[id]
		return v.Type == "record" && v.Mode == "resource" && v.Source != nil && v.Source.Kind == "record" && slices.Contains([]string{"page", "overlay"}, v.Scope) || s.Widget == "breadcrumb" && d.sharedContextRecord(id)
	}
	switch s.Widget {
	case "breadcrumb":
		b := s.Breadcrumb
		if b == nil || b.HomeLabel == "" || b.PageLabel == "" || len(b.HomeLabel) > c.MaxLabelBytes || len(b.PageLabel) > c.MaxLabelBytes || s.CollectionVariable != "" || s.RecordVariable == "" && (b.LabelField != "" || s.Object != (AssetRef{})) || s.RecordVariable != "" && (!record(s.RecordVariable) || b.LabelField == "" || len(b.LabelField) > 256) {
			return fmt.Errorf("breadcrumb needs bounded labels and an optional original record title")
		}
		found := false
		for _, e := range d.Events {
			if n := e.Navigation(); e.Source == s.ID && e.Control == "home" && e.Event == "click" && e.Only("navigate") && n != nil && len(n.Inputs) == 0 && len(n.Results) == 0 {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("breadcrumb home needs its original page navigation event")
		}
	case "avatar-stack":
		a := s.Avatar
		if a == nil || a.LabelField == "" || len(a.LabelField) > 256 || len(a.DetailFields) > c.MaxDetailFields || s.RecordVariable != "" {
			return fmt.Errorf("avatar stack needs explicit original title and bounded details")
		}
		seen := map[string]bool{a.LabelField: true}
		for _, f := range a.DetailFields {
			if f == "" || len(f) > 256 || seen[f] {
				return fmt.Errorf("avatar fields must be unique original names")
			}
			seen[f] = true
		}
		window := func(id string) (PageQuery, bool) {
			v := d.Variables[id]
			if v.Type != "object-set" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) {
				return PageQuery{}, false
			}
			q, ok := d.Queries[v.Source.Query]
			return q, ok && q.Query != nil && q.Query.Ref.Kind == AssetQuery && q.Limit == c.MaxAvatarWindow && q.Offset == 0 && slices.Equal(q.Sort, []string{"id"}) && q.ItemOwner == "" && q.Set == nil && q.Search == nil && len(q.Conditions) == 0
		}
		all, ok := window(s.CollectionVariable)
		if !ok || all.For != nil {
			return fmt.Errorf("avatar stack needs its retained all-person query with six records, zero offset and ID ordering")
		}
		if (a.ContextVariable == "") != (a.ContextCollectionVariable == "") {
			return fmt.Errorf("avatar context needs both original record and contextual query")
		}
		if a.ContextVariable != "" {
			q, ok := window(a.ContextCollectionVariable)
			if !ok || !record(a.ContextVariable) || q.Object != all.Object || q.For == nil || q.For.Variable != a.ContextVariable || len(q.For.Literal) > 0 {
				return fmt.Errorf("avatar context query must consume the actual original record")
			}
		}
	case "static-image":
		if s.Image == nil || s.Object != (AssetRef{}) || s.RecordVariable != "" || s.CollectionVariable != "" || !ValidPageImageURL(s.Image.URL) || s.Image.Caption != nil && len(*s.Image.Caption) > c.MaxCaptionBytes || s.Image.Height != nil && (math.IsNaN(*s.Image.Height) || math.IsInf(*s.Image.Height, 0) || *s.Image.Height < 0 || *s.Image.Height > float64(c.MaxImageHeight)) {
			return fmt.Errorf("static image needs a bounded literal URL, caption and height without record bindings")
		}
	}
	return nil
}

// ValidPageImageURL checks literal browser image sources. It never fetches,
// proxies or freezes remote bytes. Empty URL is the original unconfigured state.
func ValidPageImageURL(raw string) bool {
	if raw == "" {
		return true
	}
	if len(raw) > pageWidgets.Runtime.ContextViews.MaxURLBytes || !utf8.ValidString(raw) {
		return false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) {
		return false
	}
	for _, text := range []string{raw, decoded} {
		for _, r := range text {
			if r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
				return false
			}
		}
	}
	if strings.HasPrefix(raw, "/") {
		return !strings.HasPrefix(raw, "//") && !strings.HasPrefix(decoded, "//")
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || u.Hostname() == "" {
		return false
	}
	if strings.Contains(u.Host, "[") && net.ParseIP(u.Hostname()) == nil {
		return false
	}
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > 65535 {
			return false
		}
	}
	return true
}
func visibleContextField(info EntityInfo, name string, title bool) bool {
	if name == "id" {
		return true
	}
	f, ok := info.Field(name)
	types := []string{"text", "longtext", "choice", "reference", "integer", "decimal", "money", "date", "datetime", "boolean"}
	if title {
		types = []string{"text", "longtext", "choice", "reference"}
	}
	return ok && slices.Contains(types, f.Type)
}
func (s Section) CheckContextViews(info EntityInfo) error {
	if s.Widget == "breadcrumb" && s.RecordVariable != "" && (s.Breadcrumb == nil || !visibleContextField(info, s.Breadcrumb.LabelField, true)) {
		return fmt.Errorf("breadcrumb title needs a visible original title field or ID")
	}
	if s.Widget == "avatar-stack" {
		if s.Avatar == nil || !visibleContextField(info, s.Avatar.LabelField, true) {
			return fmt.Errorf("avatar title needs a visible original title field or ID")
		}
		for _, f := range s.Avatar.DetailFields {
			if !visibleContextField(info, f, false) {
				return fmt.Errorf("avatar detail field is unavailable")
			}
		}
	}
	return nil
}
func (p Page) CheckContextViewBinding(s Section) error {
	if s.Widget == "breadcrumb" && s.RecordVariable != "" {
		object := s.Object
		if object.Name == "" {
			object = p.Object
		}
		if original := p.RecordResourceObject(s.RecordVariable); original.Name == "" || original != object {
			return fmt.Errorf("breadcrumb object differs from its actual original producer")
		}
	}
	if s.Widget == "avatar-stack" {
		if p.Document == nil || s.Avatar == nil {
			return fmt.Errorf("avatar document is unavailable")
		}
		object := s.Object
		if object.Name == "" {
			object = p.Object
		}
		for _, id := range []string{s.CollectionVariable, s.Avatar.ContextCollectionVariable} {
			if id == "" {
				continue
			}
			v := p.Document.Variables[id]
			if v.Source == nil || p.Document.Queries[v.Source.Query].Object != object {
				return fmt.Errorf("avatar window differs from the original person object")
			}
		}
		if s.Avatar.ContextVariable != "" && p.RecordResourceObject(s.Avatar.ContextVariable).Name == "" {
			return fmt.Errorf("avatar context needs its actual original producer")
		}
	}
	return nil
}

// Retained named queries own their parent reference and may not replace ID order.
func (p Page) CheckAvatarQuery(id string, named *Definition, info EntityInfo) error {
	if p.Document == nil {
		return nil
	}
	for _, s := range p.Sections {
		if s.Widget != "avatar-stack" || s.Avatar == nil {
			continue
		}
		for _, binding := range []string{s.CollectionVariable, s.Avatar.ContextCollectionVariable} {
			v := p.Document.Variables[binding]
			if binding == "" || v.Source == nil || v.Source.Query != id {
				continue
			}
			if named == nil || named.Query == nil || named.Query.Limit != 0 && named.Query.Limit < pageWidgets.Runtime.ContextViews.MaxAvatarWindow || len(named.Query.Sort) > 0 && !slices.Equal(named.Query.Sort, []string{"id"}) {
				return fmt.Errorf("avatar requires its retained named query with compatible ID ordering")
			}
			if binding == s.CollectionVariable {
				if named.Query.By != "" {
					return fmt.Errorf("avatar all-person query cannot require context")
				}
				continue
			}
			f, ok := info.Field(named.Query.By)
			if !ok || f.Type != "reference" || f.Ref != p.RecordResourceObject(s.Avatar.ContextVariable).Name {
				return fmt.Errorf("avatar contextual query needs the original typed reference parent")
			}
		}
	}
	return nil
}
