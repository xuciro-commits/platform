package host

import (
	"context"
	"time"

	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	knowledgeindex "platformserver/services/ai/retrieval"
	controlowner "platformserver/services/security/control"
)

const embeddingActiveSpace = "knowledge/embedding-active-space"

type pair[A, B any] struct {
	first  A
	second B
}

func (t *Tenant) knowledgeRuntime() knowledgeindex.Runtime {
	source := t.readOriginOrSelf()
	return knowledgeindex.Runtime{
		Tenant: t.ID, Available: t.knowledge != nil, AI: t.ai != nil, Index: t.index,
		Documents: t.automation(platform.KnowledgeApp, false),
		Files: func() (platform.Caller, bool) {
			if t.App(platform.FileApp) == nil {
				return platform.Caller{}, false
			}
			return t.automation(platform.FileApp, false), true
		},
		Projection: t.Projection, SyncStore: source.Projection,
		Page: func(typ string, offset int) ([]any, int, *kernel.Error) {
			page, err := t.objectOwner(t.Projection).Records(t.host(), typ, platform.Query{Sort: []string{"id"}, Offset: offset, Limit: 500}, time.Now())
			return page.Records, page.Total, err
		},
		FilesStore: t.files(), Store: t.Log(),
		Read: func(read func(knowledgeindex.Runtime) *kernel.Error) *kernel.Error {
			_, refusal := recordRead(source, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
				return struct{}{}, read(t.knowledgeRuntime())
			})
			return refusal
		},
		Current: func() bool {
			image := source.readImage.Load()
			return image != nil && image.records == source.Projection.Generation()
		},
		Host:     t.host,
		Readable: func(m platform.Member, scope string, at time.Time) bool { return t.readable(m, scope, at) },
		Setting: func(name string) string {
			return readOnly(t, func(t *Tenant) string { return t.setting(t.automation(platform.KnowledgeApp, false), name) })
		},
		ActiveSpace: func() string {
			value, _ := t.settings.Get(embeddingActiveSpace)
			return value
		},
		Model: func(name string) (platform.AIModel, platform.AIProvider, *kernel.Error) {
			value, err := recordRead(t, context.Background(), func(t *Tenant) (pair[platform.AIModel, platform.AIProvider], *kernel.Error) {
				model, provider, refusal := t.ai.Model(name)
				return pair[platform.AIModel, platform.AIProvider]{model, provider}, refusal
			})
			return value.first, value.second, err
		},
		Resolve: func(value string) (platform.AIProvider, *kernel.Error) {
			_, provider, refusal := t.modelResolver().Resolve("embedding", value, platform.ModelOptions{})
			return provider, refusal
		},
		Stage: func(binding platform.AIModelBinding, pv platform.AIProvider) *kernel.Error {
			_, refusal := t.modelResolver().Stage(binding, pv, platform.AIStageRuntime)
			return refusal
		},
		EmbedModel: func(name string, model platform.AIModel, pv platform.AIProvider, input []string) ([][]float32, error) {
			return t.gatewayRuntime().Embed(platform.Member{ID: "app:" + platform.KnowledgeApp, Tenant: t.ID}, name, model, pv, input, time.Now())
		},
		Stopped: func() bool { return source.businessStopped() },
		Changed: func() { source.operationsChanged(platform.KnowledgeApp) },
	}
}

func (t *Tenant) knowledgeSourceReadable(store *records.Store, m platform.Member, id, field string) bool {
	return t.knowledgeRuntime().SourceReadable(store, m, id, field)
}

func (t *Tenant) vectorSpace(name string) string {
	return t.knowledgeRuntime().VectorSpace(name)
}

func (t *Tenant) KnowledgeIndexStatus(ctx context.Context, m platform.Member) (platform.KnowledgeIndexStatus, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (platform.KnowledgeIndexStatus, *kernel.Error) {
		c := platform.NewCaller(runtime{t: t}, m, controlowner.PlatformApp, false, false)
		if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/ai/knowledge-index", "read", []string{controlowner.Admin}, true)); err != nil {
			return platform.KnowledgeIndexStatus{}, err
		}
		return t.knowledgeRuntime().Status(), nil
	})
}

func (t *Tenant) checkEmbeddingSetting(id, value string) *kernel.Error {
	return t.knowledgeRuntime().CheckSetting(id, value)
}

func (t *Tenant) Knowledge(reader *platform.Member, app, q string, limit int, at time.Time) []platform.Passage {
	return t.knowledgeRuntime().Knowledge(reader, app, q, limit, at)
}

func (t *Tenant) Embed(at time.Time) {
	t.knowledgeRuntime().EmbedAll(at)
}

func (t *Tenant) readOriginOrSelf() *Tenant {
	if t.readOrigin != nil {
		return t.readOrigin
	}
	return t
}
