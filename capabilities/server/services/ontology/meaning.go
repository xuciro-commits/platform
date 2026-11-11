package ontology

import (
	"slices"
	"strings"

	"platformserver/platform"
)

func Meaning(infos []platform.EntityInfo) string {
	var b strings.Builder
	for _, info := range infos {
		var lines []string
		for _, f := range info.Fields {
			if f.Help == "" && f.Synonyms == "" && f.Example == "" {
				continue
			}
			line := "  - " + f.Name + " (" + f.Title + ")"
			if f.Help != "" {
				line += ": " + f.Help
			}
			if f.Synonyms != "" {
				line += "; also called " + f.Synonyms
			}
			if f.Example != "" {
				line += "; e.g. " + f.Example
			}
			lines = append(lines, line)
		}
		if info.Lifecycle != nil {
			for _, st := range info.Lifecycle.States {
				if st.Description != "" {
					lines = append(lines, "  - state "+st.Name+": "+st.Description)
				}
			}
		}
		if info.Description == "" && info.Synonyms == "" && len(lines) == 0 {
			continue
		}
		b.WriteString("- " + info.Type + " (" + info.Title + ")")
		if info.Description != "" {
			b.WriteString(": " + info.Description)
		}
		if info.Synonyms != "" {
			b.WriteString("; also called " + info.Synonyms)
		}
		b.WriteString("\n")
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func StripMention(q string, candidates []string) (string, bool) {
	lower := " " + strings.ToLower(strings.Join(strings.Fields(q), " ")) + " "
	slices.SortFunc(candidates, func(a, b string) int { return len(b) - len(a) })
	for _, c := range candidates {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		wide := c[0] >= 0x80
		for _, form := range []string{c + "s", c} {
			if i := strings.Index(lower, " "+form+" "); i >= 0 && !wide {
				return strings.TrimSpace(lower[:i] + " " + lower[i+len(form)+2:]), true
			}
		}
		if i := strings.Index(lower, c); wide && i >= 0 {
			return strings.TrimSpace(lower[:i] + lower[i+len(c):]), true
		}
	}
	return q, false
}

func Glossary(terms []platform.KnowledgeTerm) string {
	var b strings.Builder
	for _, x := range terms {
		b.WriteString("- " + x.Term + ": " + x.Meaning)
		if x.Synonyms != "" {
			b.WriteString(" (also: " + x.Synonyms + ")")
		}
		if x.RefersTo != "" {
			b.WriteString(" [" + x.RefersTo + "]")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
