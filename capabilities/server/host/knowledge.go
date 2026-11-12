package host

import (
	"context"
	controlowner "platformserver/services/security/control"

	"fmt"
	"io"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	knowledgeindex "platformserver/services/ai/retrieval"

	"platformserver/platform"
	"reflect"

	"time"
)

const embeddingActiveSpace = "knowledge/embedding-active-space"

func (t *Tenant) corpus() knowledgeindex.Corpus {
	c := knowledgeindex.Corpus{Documents: t.automation(platform.KnowledgeApp, false), Index: t.index,
		Types: func() []platform.EntityInfo {
			t.Projection.Lock()
			defer t.Projection.Unlock()
			var out []platform.EntityInfo
			for _, et := range t.Projection.Types() {
				out = append(out, et.Info())
			}
			return out
		},
		Page: func(typ string, offset int) ([]any, int, *kernel.Error) {
			page, err := t.recordsRead(t.host(), typ, platform.Query{Sort: []string{"id"}, Offset: offset, Limit: 500}, time.Now())
			return page.Records, page.Total, err
		},
		Row: t.knowledgeRow,

		Blob: func(hash string) (string, bool) {
			body, _, err := t.files().Get(context.Background(), t.ID+"/"+hash)
			if err != nil {
				return "", false
			}
			raw, _ := io.ReadAll(io.LimitReader(body, 1<<20))
			body.Close()
			return string(raw), true
		}}
	if t.App(platform.FileApp) != nil {
		files := t.automation(platform.FileApp, false)
		c.Files = &files
	}
	return c
}

func (t *Tenant) knowledgeSourceReadable(store *records.Store, m platform.Member, id, field string) bool {
	if m.Tenant != t.ID || field != "" && field != "title" && field != "text" {
		return false
	}
	et := store.Type(platform.KnowledgeDocumentType)
	if et == nil || et.Rows().Get(id) == nil {
		return false
	}
	d, ok := et.Rows().Get(id).Value.Interface().(platform.KnowledgeDocument)
	return ok && !d.Archived && knowledgeindex.AppsReadable(d.Apps, &m, "")
}

func (t *Tenant) embed(name string, input []string, now time.Time) ([][]float32, error) {
	type configured struct {
		model    platform.AIModel
		provider platform.AIProvider
	}
	value, err := recordRead(t, context.Background(), func(t *Tenant) (configured, *kernel.Error) {
		model, provider, refusal := t.ai.Model(name)
		return configured{model: model, provider: provider}, refusal
	})
	model, pv := value.model, value.provider
	if err != nil {
		return nil, fmt.Errorf("the model %s is not enabled", name)
	}
	binding, problem := model.Resolve("embedding", platform.ModelOptions{}, 0)
	if problem != nil {
		return nil, problem
	}
	if _, refusal := t.modelResolver().Stage(binding, pv, platform.AIStageRuntime); refusal != nil {
		return nil, refusal
	}
	return t.gateway(nil).Embed(platform.Member{ID: "app:" + platform.KnowledgeApp, Tenant: t.ID}, name, model, pv, input, now)
}

func (t *Tenant) embeddingNeedsRebuild(name string) bool {
	space, exists := t.settings.Get(embeddingActiveSpace)
	return name != "" && exists && space != "" && space != t.vectorSpace(name)
}

func (t *Tenant) vectorSpace(name string) string {
	if t.ai == nil {
		return name
	}
	m, pv, err := t.ai.Model(name)
	if err != nil || m.Revision == 0 {
		return name
	}
	digest, _ := platform.CanonicalDigest([]any{name, pv.BaseURL, pv.Wire, m.Revision, m.Capabilities})
	return name + "@" + digest
}

func (t *Tenant) KnowledgeIndexStatus(ctx context.Context, m platform.Member) (platform.KnowledgeIndexStatus, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (platform.KnowledgeIndexStatus, *kernel.Error) {
		c := platform.NewCaller(runtime{t: t}, m, controlowner.PlatformApp, false, false)
		if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/ai/knowledge-index", "read", []string{controlowner.Admin}, true)); err != nil {
			return platform.KnowledgeIndexStatus{}, err
		}
		return t.knowledgeIndexStatus(), nil
	})
}

func (t *Tenant) checkEmbeddingSetting(id, value string) *kernel.Error {
	if id != platform.KnowledgeApp+"/"+platform.SettingKnowledgeEmbeddingNext && id != platform.KnowledgeApp+"/"+platform.SettingKnowledgeEmbeddingModel {
		return nil
	}
	if value == "" {
		return nil
	}
	_, pv, err := t.modelResolver().Resolve("embedding", value, platform.ModelOptions{})
	if err != nil {
		return err
	}
	if pv.Wire != "openai" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "This provider adapter does not support embeddings")
	}
	if id == platform.KnowledgeApp+"/"+platform.SettingKnowledgeEmbeddingModel {
		status := t.knowledgeIndexStatus()
		if status.Preparing != value || !status.Ready {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Build this model's index in AI configuration before activating it")
		}
	}
	return nil
}

func (t *Tenant) knowledgeRow(typ, id string) (platform.EntityInfo, reflect.Value, bool) {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	et := t.Projection.Type(typ)
	if et == nil || et.Rows().Get(id) == nil {
		return platform.EntityInfo{}, reflect.Value{}, false
	}
	return et.Info(), et.Rows().Get(id).Value, true
}

func (t *Tenant) knowledgePassageReadableInLane(document, scope string, apps []string, reader *platform.Member, app string, now time.Time) bool {
	if scope == "" {
		return knowledgeindex.DocumentReadable(t.knowledgeRow, document, reader, app)
	}
	m := t.host()
	if reader != nil {
		m = *reader
	}
	return t.readable(m, scope, now)
}

func (t *Tenant) knowledgeContext() knowledgeindex.Context {
	source := t
	if t.readOrigin != nil {
		source = t.readOrigin
	}
	setting := func(name string) string {
		value, _ := recordRead(t, context.Background(), func(t *Tenant) (string, *kernel.Error) {
			return t.setting(t.automation(platform.KnowledgeApp, false), name), nil
		})
		return value
	}
	return knowledgeindex.Context{Tenant: t.ID, Store: t.Log(), Sync: source.syncRecords,
		Read: func(read func(knowledgeindex.Context)) {
			_, _ = recordRead(source, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) { read(t.knowledgeContext()); return struct{}{}, nil })
		},
		Visible: t.knowledgePassageReadableInLane,
		ScopeReadable: func(reader *platform.Member, scope string, at time.Time) bool {
			member := t.host()
			if reader != nil {
				member = *reader
			}
			return t.readable(member, scope, at)
		},
		Active:    func() string { return setting(platform.SettingKnowledgeEmbeddingModel) },
		Preparing: func() string { return setting(platform.SettingKnowledgeEmbeddingNext) },
		NeedsRebuild: func(name string) bool {
			value, _ := recordRead(t, context.Background(), func(t *Tenant) (bool, *kernel.Error) { return t.embeddingNeedsRebuild(name), nil })
			return value
		},
		Space: func(name string) string {
			value, _ := recordRead(t, context.Background(), func(t *Tenant) (string, *kernel.Error) { return t.vectorSpace(name), nil })
			return value
		},
		Embed: source.embed, Stopped: source.businessStopped, Changed: func() { source.operationsChanged(platform.KnowledgeApp) },
	}
}

func (t *Tenant) syncRecords() {
	source := t
	if t.readOrigin != nil {
		source = t.readOrigin
	}
	source.index.Sync(func(walked bool) ([]knowledgeindex.Source, map[string]bool, bool) {
		source.Projection.Lock()
		image := source.readImage.Load()
		if image == nil || image.records != source.Projection.Generation() {
			source.Projection.Unlock()
			return nil, nil, false
		}
		dirty := source.Projection.TakeDirty()
		source.Projection.Unlock()
		changed := map[string]bool{}
		for ref, marked := range dirty.All() {
			changed[ref] = marked
		}
		values, refusal := recordRead(source, context.Background(), func(t *Tenant) ([]knowledgeindex.Source, *kernel.Error) {
			if walked {
				return t.corpus().Changed(changed), nil
			}
			return t.corpus().All(), nil
		})
		if refusal != nil {
			source.Projection.Lock()
			for ref := range changed {
				source.Projection.MarkDirty(ref)
			}
			source.Projection.Unlock()
			return nil, nil, false
		}
		return values, changed, true
	})
}

func (t *Tenant) Knowledge(reader *platform.Member, app, q string, limit int, at time.Time) []platform.Passage {
	if t.knowledge == nil {
		return []platform.Passage{}
	}
	return t.index.Search(t.knowledgeContext(), reader, app, q, limit, at)
}
func (t *Tenant) Embed(at time.Time) {
	if t.knowledge == nil || t.ai == nil {
		return
	}
	t.index.Embed(t.knowledgeContext(), at)
}
func (t *Tenant) knowledgeIndexStatus() platform.KnowledgeIndexStatus {
	t.syncRecords()
	return t.index.Status(t.knowledgeContext())
}
