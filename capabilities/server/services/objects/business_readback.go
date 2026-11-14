package objects

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"

	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func ReadableFieldIssues(store *records.Store, member platform.Member, issues []platform.FieldIssue, readableIn func(string) bool, narrower func() *Narrower) []platform.FieldIssue {
	issues = slices.Clone(issues)
	store.Lock()
	defer store.Unlock()
	for i, issue := range issues {
		issues[i].Values = maps.Clone(issue.Values)
		if issue.Code == "business_refusal" {
			if !readableIn(issue.Record) {
				issues[i].Values = nil
				issues[i].Translations = nil
				issues[i].Message = "The business record is no longer readable"
			}
			continue
		}
		if issue.Constraint == "" {
			continue
		}
		typ, id, _ := strings.Cut(issue.Record, "/")
		et := store.Type(typ)
		if et == nil {
			issues[i].Values = nil
			continue
		}
		info := platform.ConstraintEntity(et.Info())
		if et.Rows().Get(id) != nil && !readableIn(issue.Record) {
			issues[i].Values = nil
			continue
		}
		if current := et.Rows().Get(id); current != nil && et.Info().HasReadSources() {
			narrowed, _ := narrower().Derive(et, current.Value)
			for _, derivation := range et.Info().Derived {
				paths := derivation.Fields
				if len(derivation.List) > 0 {
					paths = [][]int{derivation.List}
				}
				for _, indices := range paths {
					if reflect.DeepEqual(current.Value.FieldByIndex(indices).Interface(), narrowed.FieldByIndex(indices).Interface()) {
						continue
					}
					delete(issues[i].Values, fieldName(et.Info().Go, indices))
				}
			}
		}
		for name := range issues[i].Values {
			field, ok := info.Field(name)
			if !ok || !field.ReadsAny(member.RolesIn(info.App)) {
				delete(issues[i].Values, name)
			}
		}
	}
	return issues
}

func ReadableBusinessFact(store *records.Store, m platform.Member, f platform.BusinessRefusalFact, readable, readableIn func(string) bool, narrower func() *Narrower) platform.BusinessRefusalFact {
	f = acceptedformat.CloneBusinessFact(f)
	if f.Record != "" && !readable(f.Record) {
		f.Record = ""
	}
	if f.Route != nil && f.Route.ID != "" && !readable(f.Route.Type+"/"+f.Route.ID) {
		f.Route = nil
	}
	typ, id, _ := strings.Cut(f.Subject, "/")
	var ok bool
	var et *records.Type
	func() {
		store.Lock()
		defer store.Unlock()
		et = store.Type(typ)
		ok = et != nil && et.Rows().Get(id) != nil && readableIn(f.Subject)
	}()
	if !ok {
		f.Original = nil
		f.Inputs = nil
		return f
	}
	var original map[string]json.RawMessage
	if json.Unmarshal(f.Original, &original) != nil {
		f.Original = nil
		return f
	}
	info := et.Info()
	historical := reflect.New(info.Go).Elem()
	if json.Unmarshal(f.Original, historical.Addr().Interface()) != nil {
		f.Original = nil
		return f
	}
	if info.HasReadSources() || len(info.VersionPins) > 0 {
		narrowed, _ := narrower().Derive(et, historical)
		var rawBefore, rawAfter map[string]json.RawMessage
		_ = json.Unmarshal(platform.Raw(historical.Interface()), &rawBefore)
		_ = json.Unmarshal(platform.Raw(narrowed.Interface()), &rawAfter)
		for name := range original {
			beforeHash, _ := platform.CanonicalDigest(rawBefore[name])
			afterHash, _ := platform.CanonicalDigest(rawAfter[name])
			if beforeHash != afterHash {
				delete(original, name)
			}
		}
	}
	for name := range original {
		field, ok := info.Field(name)
		if name != "id" && (!ok || !field.ReadsAny(m.RolesIn(info.App))) {
			delete(original, name)
		}
	}
	f.Original = platform.Raw(original)
	if m.ID != f.Member {
		f.Inputs = nil
	}
	return f
}

func LocalizedFieldIssues(issues []platform.FieldIssue, language string, said func(message string) string) []platform.FieldIssue {
	issues = slices.Clone(issues)
	for i := range issues {
		if translated := issues[i].Translations[language]; translated != "" {
			issues[i].Message = translated
		} else {
			issues[i].Message = said(issues[i].Message)
		}
	}
	return issues
}
