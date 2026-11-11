package host

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	controlowner "platformserver/services/security/control"

	"encoding/json"
	"fmt"
	"io"

	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	knowledgeindex "platformserver/services/ai/retrieval"

	"platformserver/platform"
	"reflect"
	"slices"
	"strings"

	"time"
)

const embeddingActiveSpace = "knowledge/embedding-active-space"

func (t *Tenant) sources() []knowledgeindex.Source {
	var out []knowledgeindex.Source
	docs, _, _ := platform.Find[platform.KnowledgeDocument](t.automation(platform.KnowledgeApp, false), platform.Query{Limit: 100000})
	for _, d := range docs {
		out = append(out, knowledgeindex.DocumentSource(d))
	}
	out = append(out, t.fileSources()...)
	t.Projection.Lock()
	types := slices.Collect(func(yield func(*records.Type) bool) {
		for _, et := range t.Projection.Types() {
			if slices.ContainsFunc(et.Info().Fields, func(f platform.FieldInfo) bool { return f.Knowledge }) && !yield(et) {
				return
			}
		}
	})
	t.Projection.Unlock()
	for _, et := range types {
		for offset := 0; ; offset += 500 {
			page, err := t.recordsRead(t.host(), et.Info().Type, platform.Query{Sort: []string{"id"}, Offset: offset, Limit: 500}, time.Now())
			if err != nil {
				break
			}
			for _, r := range page.Records {
				out = append(out, knowledgeindex.RecordSources(et.Info(), reflect.ValueOf(r))...)
			}
			if offset+len(page.Records) >= page.Total || len(page.Records) == 0 {
				break
			}
		}
	}
	return out
}

func (t *Tenant) sourcesOf(changed map[string]bool) []knowledgeindex.Source {
	var out []knowledgeindex.Source
	for ref := range changed {
		typ, id, _ := strings.Cut(ref, "/")
		switch typ {
		case platform.KnowledgeDocumentType:
			if d, known := platform.Get[platform.KnowledgeDocument](t.automation(platform.KnowledgeApp, false), id); known && !d.Archived {
				out = append(out, knowledgeindex.DocumentSource(d))
			}
			about, _ := json.Marshal([]any{[]any{"target", "=", ref}})
			attached, _, _ := platform.Find[platform.File](t.automation(platform.FileApp, false), platform.Query{Domain: about, Limit: 200})
			for _, f := range attached {
				if s, ok := t.fileSource(f); ok {
					out = append(out, s)
				}
			}
		case platform.FileType:
			if f, known := platform.Get[platform.File](t.automation(platform.FileApp, false), id); known && !f.Archived {
				if s, ok := t.fileSource(f); ok {
					out = append(out, s)
				}
			}
		default:
			var et *records.Type
			var v reflect.Value
			func() {
				t.Projection.Lock()
				defer t.Projection.Unlock()
				et = t.Projection.Type(typ)
				if et != nil && et.Rows().Get(id) != nil {
					v = et.Rows().Get(id).Value
				}
			}()
			if et == nil || !v.IsValid() || v.FieldByName("Record").Interface().(platform.Record).Archived {
				continue
			}
			out = append(out, knowledgeindex.RecordSources(et.Info(), v)...)
		}
	}
	return out
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
	if pv.Wire == "anthropic" {
		return nil, fmt.Errorf("the provider %s does not embed", pv.ID)
	}
	if !t.breakers.Allow("ai:"+pv.ID, now, false) {
		return nil, fmt.Errorf("the provider %s failed repeatedly; embedding waits", pv.ID)
	}
	inputTokens := 0
	for _, text := range input {
		inputTokens += len(text) + 32
	}
	call := "embedding:" + rand.Text()
	identity, _ := json.Marshal(input)
	digest := fmt.Sprintf("%x", sha256.Sum256(identity))
	if why := t.reserveCall(platform.Member{ID: "app:" + platform.KnowledgeApp, Tenant: t.ID}, model, platform.ChatRequest{Call: call, RequestHash: digest}, inputTokens, 0, now); why != nil {
		return nil, why
	}
	if !t.breakers.Allow("ai:"+pv.ID, now, true) {
		if refusal := t.releaseCall(call, now); refusal != nil {
			return nil, refusal
		}
		return nil, fmt.Errorf("the provider %s failed repeatedly; embedding waits", pv.ID)
	}
	started := time.Now()
	dispatched := false
	status, answer, failure := t.modelProvider(pv, func() { dispatched = true }).Embeddings(pv, model.Model, input)
	t.breakers.Report("ai:"+pv.ID, failure == nil && status < 500 && status != http.StatusTooManyRequests, now)
	if !dispatched {
		if why := t.releaseCall(call, now); why != nil {
			return nil, why
		}
		if failure != nil {
			return nil, failure
		}
		return nil, fmt.Errorf("The embedding call was not dispatched")
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Usage struct {
			Prompt *int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	u := platform.AIUsage{Call: call, At: now, Member: "app:" + platform.KnowledgeApp, Model: name, Millis: time.Since(started).Milliseconds(), Outcome: "ok"}
	switch {
	case failure != nil:
		u.Outcome = failure.Detail
	case status/100 != 2 || json.Unmarshal(answer, &out) != nil || len(out.Data) != len(input):
		u.Outcome = fmt.Sprintf("HTTP %d: no embeddings", status)
	}
	if out.Usage.Prompt != nil {
		u.Input, u.TokensReported = *out.Usage.Prompt, true
	} else {
		for _, text := range input {
			u.Input += platform.EstimateModelTokens(text)
		}
		u.TokensEstimated = u.Input > 0
	}
	if err := t.meter(platform.Member{ID: u.Member, Tenant: t.ID}, u); err != nil {
		return nil, fmt.Errorf("the embedding usage was not accepted: %v", err)
	}
	if u.Outcome != "ok" {
		return nil, fmt.Errorf("%s", u.Outcome)
	}
	if u.TokensReported && u.Input > inputTokens {
		return nil, fmt.Errorf("The provider reported usage above the reserved token bound")
	}
	vs := make([][]float32, len(input))
	for _, d := range out.Data {
		if d.Index >= 0 && d.Index < len(vs) {
			vs[d.Index] = d.Embedding
		}
	}
	if err := knowledgeindex.ConsistentVectors(vs, 0); err != nil {
		return nil, err
	}
	return vs, nil
}

func (t *Tenant) fileSources() []knowledgeindex.Source {
	if t.app(platform.FileApp) == nil {
		return nil
	}
	attached, _, _ := platform.Find[platform.File](t.automation(platform.FileApp, false), platform.Query{Limit: 100000})
	var out []knowledgeindex.Source
	for _, f := range attached {
		if s, ok := t.fileSource(f); ok {
			out = append(out, s)
		}
	}
	return out
}

func (t *Tenant) fileSource(f platform.File) (knowledgeindex.Source, bool) {
	if t.app(platform.FileApp) == nil || f.Archived || !strings.HasPrefix(f.ContentType, "text/") || f.Size > 1<<20 {
		return knowledgeindex.Source{}, false
	}
	{
		typ, id, _ := strings.Cut(f.Target, "/")
		var apps []string
		switch {
		case typ == platform.KnowledgeDocumentType:
			d, known := platform.Get[platform.KnowledgeDocument](t.automation(platform.KnowledgeApp, false), id)
			if !known || d.Archived {
				return knowledgeindex.Source{}, false
			}
			apps = d.Apps
		default:
			t.Projection.Lock()
			et := t.Projection.Type(typ)
			t.Projection.Unlock()
			if et == nil || !et.Info().KnowledgeFiles {
				return knowledgeindex.Source{}, false
			}
			apps = []string{et.Info().App}
		}
		text, ok := t.index.Text(f.Hash, func() (string, bool) {
			body, _, err := t.files().Get(context.Background(), t.ID+"/"+f.Hash)
			if err != nil {
				return "", false
			}
			raw, _ := io.ReadAll(io.LimitReader(body, 1<<20))
			body.Close()
			return string(raw), true
		})
		if !ok {
			return knowledgeindex.Source{}, false
		}
		scope := f.Target
		if typ == platform.KnowledgeDocumentType {
			scope = ""
		}
		return knowledgeindex.Source{Key: platform.FileType + "/" + f.ID, Title: f.Name, Text: text,
			Revision: f.Hash + "/" + f.Target + "/" + strings.Join(apps, ","), Scope: scope, Apps: apps}, true
	}
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

func (t *Tenant) knowledgePassageReadableInLane(document, scope string, apps []string, reader *platform.Member, app string, now time.Time) bool {
	if scope == "" {
		id, isDocument := strings.CutPrefix(document, platform.KnowledgeDocumentType+"/")
		t.Projection.Lock()
		defer t.Projection.Unlock()
		if !isDocument {
			fileID, fileSource := strings.CutPrefix(document, platform.FileType+"/")
			files := t.Projection.Type(platform.FileType)
			if !fileSource || files == nil {
				return false
			}
			r := files.Rows().Get(fileID)
			if r == nil {
				return false
			}
			file := r.Value.Interface().(platform.File)
			if file.Archived {
				return false
			}
			id, isDocument = strings.CutPrefix(file.Target, platform.KnowledgeDocumentType+"/")
			if !isDocument {
				return false
			}
		}
		et := t.Projection.Type(platform.KnowledgeDocumentType)
		if et == nil {
			return false
		}
		r := et.Rows().Get(id)
		if r == nil {
			return false
		}
		d := r.Value.Interface().(platform.KnowledgeDocument)
		return !d.Archived && knowledgeindex.AppsReadable(d.Apps, reader, app)
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
				return t.sourcesOf(changed), nil
			}
			return t.sources(), nil
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
