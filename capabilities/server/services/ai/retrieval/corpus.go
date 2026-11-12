package knowledge

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"

	"platformkernel/kernel"
	"platformserver/platform"
)

type Corpus struct {
	Documents platform.Caller
	Files     *platform.Caller
	Types     func() []platform.EntityInfo
	Page      func(typ string, offset int) ([]any, int, *kernel.Error)
	Row       func(typ, id string) (platform.EntityInfo, reflect.Value, bool)
	Blob      func(hash string) (string, bool)
	Index     *Index
}

func (c Corpus) All() []Source {
	var out []Source
	docs, _, _ := platform.Find[platform.KnowledgeDocument](c.Documents, platform.Query{Limit: 100000})
	for _, d := range docs {
		out = append(out, DocumentSource(d))
	}
	if c.Files != nil {
		attached, _, _ := platform.Find[platform.File](*c.Files, platform.Query{Limit: 100000})
		for _, f := range attached {
			if s, ok := c.file(f); ok {
				out = append(out, s)
			}
		}
	}
	for _, info := range c.Types() {
		if !slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool { return f.Knowledge }) {
			continue
		}
		for offset := 0; ; offset += 500 {
			records, total, err := c.Page(info.Type, offset)
			if err != nil {
				break
			}
			for _, r := range records {
				out = append(out, RecordSources(info, reflect.ValueOf(r))...)
			}
			if offset+len(records) >= total || len(records) == 0 {
				break
			}
		}
	}
	return out
}

func (c Corpus) Changed(changed map[string]bool) []Source {
	var out []Source
	for ref := range changed {
		typ, id, _ := strings.Cut(ref, "/")
		switch typ {
		case platform.KnowledgeDocumentType:
			if d, known := platform.Get[platform.KnowledgeDocument](c.Documents, id); known && !d.Archived {
				out = append(out, DocumentSource(d))
			}
			if c.Files == nil {
				continue
			}
			about, _ := json.Marshal([]any{[]any{"target", "=", ref}})
			attached, _, _ := platform.Find[platform.File](*c.Files, platform.Query{Domain: about, Limit: 200})
			for _, f := range attached {
				if s, ok := c.file(f); ok {
					out = append(out, s)
				}
			}
		case platform.FileType:
			if c.Files == nil {
				continue
			}
			if f, known := platform.Get[platform.File](*c.Files, id); known && !f.Archived {
				if s, ok := c.file(f); ok {
					out = append(out, s)
				}
			}
		default:
			info, v, ok := c.Row(typ, id)
			if !ok || v.FieldByName("Record").Interface().(platform.Record).Archived {
				continue
			}
			out = append(out, RecordSources(info, v)...)
		}
	}
	return out
}

func (c Corpus) file(f platform.File) (Source, bool) {
	if f.Archived || !strings.HasPrefix(f.ContentType, "text/") || f.Size > 1<<20 {
		return Source{}, false
	}
	typ, id, _ := strings.Cut(f.Target, "/")
	var apps []string
	if typ == platform.KnowledgeDocumentType {
		d, known := platform.Get[platform.KnowledgeDocument](c.Documents, id)
		if !known || d.Archived {
			return Source{}, false
		}
		apps = d.Apps
	} else {
		types := c.Types()
		i := slices.IndexFunc(types, func(info platform.EntityInfo) bool { return info.Type == typ })
		if i < 0 || !types[i].KnowledgeFiles {
			return Source{}, false
		}
		apps = []string{types[i].App}
	}
	text, ok := c.Index.Text(f.Hash, func() (string, bool) { return c.Blob(f.Hash) })
	if !ok {
		return Source{}, false
	}
	scope := f.Target
	if typ == platform.KnowledgeDocumentType {
		scope = ""
	}
	return Source{Key: platform.FileType + "/" + f.ID, Title: f.Name, Text: text,
		Revision: f.Hash + "/" + f.Target + "/" + strings.Join(apps, ","), Scope: scope, Apps: apps}, true
}
