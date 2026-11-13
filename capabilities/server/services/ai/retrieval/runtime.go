package knowledge

import (
	"context"
	"io"
	"reflect"
	"time"

	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

const (
	EmbeddingActiveSpace = "knowledge/embedding-active-space"
)

type Runtime struct {
	Tenant      string
	Available   bool
	AI          bool
	Index       *Index
	Documents   platform.Caller
	Files       func() (platform.Caller, bool)
	Projection  *records.Store
	SyncStore   *records.Store
	Page        func(string, int) ([]any, int, *kernel.Error)
	FilesStore  platform.FileStore
	Store       VectorStore
	Read        func(func(Runtime) *kernel.Error) *kernel.Error
	Current     func() bool
	Reader      func(*platform.Member) platform.Member
	Readable    func(platform.Member, string, time.Time) bool
	Setting     func(string) string
	ActiveSpace func() string
	Model       func(string) (platform.AIModel, platform.AIProvider, *kernel.Error)
	Resolve     func(string) (platform.AIProvider, *kernel.Error)
	Stage       func(platform.AIModelBinding, platform.AIProvider) *kernel.Error
	EmbedModel  func(string, platform.AIModel, platform.AIProvider, []string) ([][]float32, error)
	Stopped     func() bool
	Changed     func()
}

func (r Runtime) Corpus() Corpus {
	c := Corpus{Documents: r.Documents, Index: r.Index, Types: r.types, Page: r.Page, Row: r.row, Blob: r.blob}
	if files, ok := r.Files(); ok {
		c.Files = &files
	}
	return c
}

func (r Runtime) types() []platform.EntityInfo {
	r.Projection.Lock()
	defer r.Projection.Unlock()
	var out []platform.EntityInfo
	for _, et := range r.Projection.Types() {
		out = append(out, et.Info())
	}
	return out
}

func (r Runtime) row(typ, id string) (platform.EntityInfo, reflect.Value, bool) {
	r.Projection.Lock()
	defer r.Projection.Unlock()
	et := r.Projection.Type(typ)
	if et == nil || et.Rows().Get(id) == nil {
		return platform.EntityInfo{}, reflect.Value{}, false
	}
	return et.Info(), et.Rows().Get(id).Value, true
}

func (r Runtime) blob(hash string) (string, bool) {
	body, _, err := r.FilesStore.Get(context.Background(), r.Tenant+"/"+hash)
	if err != nil {
		return "", false
	}
	raw, _ := io.ReadAll(io.LimitReader(body, 1<<20))
	body.Close()
	return string(raw), true
}

func (r Runtime) SourceReadable(store *records.Store, m platform.Member, id, field string) bool {
	if m.Tenant != r.Tenant || field != "" && field != "title" && field != "text" {
		return false
	}
	et := store.Type(platform.KnowledgeDocumentType)
	if et == nil || et.Rows().Get(id) == nil {
		return false
	}
	d, ok := et.Rows().Get(id).Value.Interface().(platform.KnowledgeDocument)
	return ok && !d.Archived && AppsReadable(d.Apps, &m, "")
}

func (r Runtime) Embed(name string, input []string, now time.Time) ([][]float32, error) {
	return Embedder{
		Model: func() (platform.AIModel, platform.AIProvider, *kernel.Error) {
			return r.Model(name)
		},
		Stage: r.Stage,
		Embed: func(model platform.AIModel, pv platform.AIProvider, input []string) ([][]float32, error) {
			return r.EmbedModel(name, model, pv, input)
		},
	}.Vectors(name, input)
}

func (r Runtime) NeedsRebuild(name string) bool {
	space := r.ActiveSpace()
	return name != "" && space != "" && space != r.VectorSpace(name)
}

func (r Runtime) VectorSpace(name string) string {
	if !r.AI {
		return name
	}
	return VectorSpace(name, r.Model)
}

func (r Runtime) ScopeReadable(reader *platform.Member, scope string, at time.Time) bool {
	return r.Readable(r.Reader(reader), scope, at)
}

func (r Runtime) PassageReadable(document, scope string, apps []string, reader *platform.Member, app string, now time.Time) bool {
	if scope == "" {
		return DocumentReadable(r.row, document, reader, app)
	}
	return r.ScopeReadable(reader, scope, now)
}

func (r Runtime) Context() Context {
	return Context{Tenant: r.Tenant, Store: r.Store, Sync: r.Sync,
		Read: func(read func(Context)) {
			r.Read(func(current Runtime) *kernel.Error {
				read(current.Context())
				return nil
			})
		},
		Visible:       r.PassageReadable,
		ScopeReadable: r.ScopeReadable,
		Active:        func() string { return r.Setting(platform.SettingKnowledgeEmbeddingModel) },
		Preparing:     func() string { return r.Setting(platform.SettingKnowledgeEmbeddingNext) },
		NeedsRebuild:  r.NeedsRebuild,
		Space:         r.VectorSpace,
		Embed:         r.Embed,
		Stopped:       r.Stopped,
		Changed:       r.Changed,
	}
}

func (r Runtime) Sync() {
	r.Index.Sync(Changes{
		Take: func() (map[string]bool, bool) {
			r.SyncStore.Lock()
			defer r.SyncStore.Unlock()
			if !r.Current() {
				return nil, false
			}
			changed := map[string]bool{}
			for ref, marked := range r.SyncStore.TakeDirty().All() {
				changed[ref] = marked
			}
			return changed, true
		},
		Restore: func(changed map[string]bool) {
			r.SyncStore.Lock()
			defer r.SyncStore.Unlock()
			for ref := range changed {
				r.SyncStore.MarkDirty(ref)
			}
		},
		Corpus: func(changed map[string]bool, walked bool) ([]Source, *kernel.Error) {
			var out []Source
			refusal := r.Read(func(current Runtime) *kernel.Error {
				if walked {
					out = current.Corpus().Changed(changed)
				} else {
					out = current.Corpus().All()
				}
				return nil
			})
			return out, refusal
		},
	}.Load)
}

func (r Runtime) Knowledge(reader *platform.Member, app, q string, limit int, at time.Time) []platform.Passage {
	if !r.Available {
		return []platform.Passage{}
	}
	return r.Index.Search(r.Context(), reader, app, q, limit, at)
}

func (r Runtime) EmbedAll(at time.Time) {
	if !r.Available || !r.AI {
		return
	}
	r.Index.Embed(r.Context(), at)
}

func (r Runtime) Status() platform.KnowledgeIndexStatus {
	r.Sync()
	return r.Index.Status(r.Context())
}

func (r Runtime) CheckSetting(id, value string) *kernel.Error {
	return EmbeddingSetting{
		Resolve: func(value string) (platform.AIProvider, *kernel.Error) {
			return r.Resolve(value)
		},
		Status: r.Status,
	}.Check(id, value)
}
