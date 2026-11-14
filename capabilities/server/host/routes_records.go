package host

import (
	"encoding/json"
	"io"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	transport "platformserver/kernel/transport"
	"platformserver/platform"
	pageowner "platformserver/services/apps/pages"
	automation "platformserver/services/automate/runtime"
	"platformserver/services/objects"
	"platformserver/services/resources/authoring"
	enterprisemodel "platformserver/services/security/organization"
	"reflect"
	"strings"
)

func enterpriseModel(t *Tenant) platform.EnterpriseQueries {
	app, _ := t.App(enterprisemodel.ID).(platform.EnterpriseQueries)
	return app
}

func (h *Host) routesRecords(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "POST /v1/page-model/read", Summary: "Read a published model binding or builder preview through the original record and rule owners", Body: pageowner.PageModelRead{}, Answer: pageowner.PageModelResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		request, ok := transport.DecodeOne[pageowner.PageModelRead](w, r, 1<<20)
		if !ok {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A model read needs one bounded request"))
			return
		}
		value, err := t.pageModels().Read(m, *request, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		owner, _, _ := strings.Cut(value.Object, ".")
		WriteJSON(w, http.StatusOK, struct {
			pageowner.PageModelResult
			Fields any `json:"fields"`
		}{value, t.i18n.TranslateInApp(value.Fields, owner, t.Language(m, r))})
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/page-model/rules", Summary: "List original action criteria available to the builder model inspector", Answer: []pageowner.PageModelRule{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) ([]pageowner.PageModelRule, *kernel.Error) {
		c := platform.NewCaller(runtime{t: t}, m, "build", false, false)
		if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/page-model/rules", "read", []string{"builder"}, true)); err != nil {
			return nil, err
		}
		return pageowner.ModelRules(t.definitions, t.modelCriteria), nil
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/record-sets/{type}/{id}/{name}", Summary: "Read a bounded declared collection through its original owner", Answer: platform.RecordSetResult{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (platform.RecordSetResult, *kernel.Error) {
		info, ok := t.Entity(r.PathValue("type"))
		if !ok {
			return platform.RecordSetResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Collection object is unavailable")
		}
		return platform.NewCaller(runtime{t: t}, m, info.App, false, false).ReadRecordSet(info.Type, r.PathValue("id"), r.PathValue("name"), h.Now())
	}))
	rt.recordRead(transport.Route{Pattern: "POST /v1/relation-migrations/preview", Summary: "Preview an explicit migration of historical links into their endpoint owner's records", Body: platform.AssetBinding{}, Answer: platform.RelationMigrationPlan{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var binding platform.AssetBinding
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&binding) != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid relationship binding"))
			return
		}
		plan, err := t.relationMigrationPlan(t.Projection, platform.NewCaller(runtime{t: t}, m, binding.Ref.App, false, false), binding, h.Now())
		transport.Answer(w, plan, err)
	})
	rt.handle(transport.Route{Pattern: "POST /v1/files", Summary: "Upload a file's bytes (the body; query name; Content-Type); answers its SHA-256 to attach with files.file.attach", Query: []transport.Param{{Name: "name", Description: "The file's name"}}, Body: []byte{}, Answer: Upload{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		up, status, err := t.Upload(m, r.URL.Query().Get("name"), r.Header.Get("Content-Type"), r.Body, h.Now())
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		WriteJSON(w, http.StatusOK, up)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/files/{id}", Summary: "Download a file attached to a record the caller may read"}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		view, refusal := t.recordOf(m, platform.FileType, r.PathValue("id"), h.Now())
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		file := view.Record.(platform.File)
		t.downloadBytes(w, file.Hash, file.Name, file.ContentType)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/media-sets/{id}/items/{hash}", Summary: "Download an item from a readable media set or retained version", Query: []transport.Param{{Name: "version", Description: "A retained version; latest when omitted"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		t.downloadMediaItem(w, r, m, h.Now())
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/chain/{type}/{id}", Summary: "The chain an agent run or a flow instance belongs to: its record, flows, runs and the effects they caused", Answer: automation.Chain{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (automation.Chain, *kernel.Error) {
		return t.chainRead(m, r.PathValue("type")+"/"+r.PathValue("id"), h.Now())
	}))
	rt.handle(transport.Route{Pattern: "POST /v1/import/{type}", Summary: "Import records from CSV: a header of field names with an id column; each row is the type's generated create or edit as the caller", Query: []transport.Param{{Name: "preview", Description: "true: check each row and apply none"}}, Body: []byte{}, Answer: []platform.ImportRow{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) ([]platform.ImportRow, *kernel.Error) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		rows, err := t.Import(m, r.PathValue("type"), body, r.URL.Query().Get("preview") == "true", h.Now())
		if err == nil {
			objects.LocalizeImportRows(rows, func(issues []platform.FieldIssue) []platform.FieldIssue {
				return t.localizedFieldIssues(m, issues, t.Language(m, r))
			})
		}
		return rows, err
	}))
	exportCSV := func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant, q platform.Query) {
		out, err := t.exportRead(m, r.PathValue("type"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		transport.WriteCSV(w, r.PathValue("type"), out)
	}
	rt.recordRead(transport.Route{Pattern: "GET /v1/export/{type}", Summary: "The records a list shows the caller, as CSV", Query: []transport.Param{{Name: "domain", Description: "Filters in the prefix form"}, {Name: "search", Description: "Words to find"}, {Name: "sort", Description: "Fields, comma-separated"}, {Name: "archived", Description: "true: archived records too"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		exportCSV(w, r, m, t, objects.QueryFromValues(r.URL.Query()))
	})
	rt.recordRead(transport.Route{Pattern: "POST /v1/export/{type}", Summary: "Export the caller's complete authorized record query as CSV, including sets, relations and business time", Body: platform.Query{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		q, ok := transport.DecodeOne[platform.Query](w, r, platform.QuerySetMaxBytes)
		if !ok || q.Limit < 0 || q.Offset < 0 {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		exportCSV(w, r, m, t, *q)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/records/{type}", Summary: "A page of an entity type's records within the caller's scope", Answer: RecordPage{}, Query: []transport.Param{{Name: "domain", Description: "Filters in the prefix form, JSON: [[\"stage\",\"=\",\"open\"]]"}, {Name: "search", Description: "Words to find"}, {Name: "sort", Description: "Fields, comma-separated; -field for descending"}, {Name: "offset", Description: "Records to skip"}, {Name: "limit", Description: "Records in the page"}, {Name: "archived", Description: "true: archived records too"}}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (RecordPage, *kernel.Error) {
		page, err := t.objectOwner(t.Projection).Records(m, r.PathValue("type"), objects.PagedQueryFromValues(r.URL.Query()), h.Now())
		if err == nil && r.PathValue("type") == build.ObjectType {
			page, err = t.objectDraftPage(m, page)
		}
		return page, err
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/enterprise-usage", Summary: "Uses preventing removal or closure of an enterprise element", Answer: enterprisemodel.EnterpriseUsage{}, Query: []transport.Param{{Name: "element", Description: "The enterprise element"}, {Name: "until", Description: "Proposed closure day"}}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (enterprisemodel.EnterpriseUsage, *kernel.Error) {
		return t.enterpriseUsageRead(m, r.URL.Query().Get("element"), r.URL.Query().Get("until"), h.Now())
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/enterprise-references", Summary: "Records that name an enterprise element, at any nesting depth, by entity type and page", Answer: []records.EnterpriseReferenceGroup{}, Query: []transport.Param{{Name: "element", Description: "The element's id"}, {Name: "offset", Description: "Records to skip, by column"}, {Name: "limit", Description: "Records per column (default 50, at most 200)"}}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) ([]records.EnterpriseReferenceGroup, *kernel.Error) {
		offset, limit := transport.PageBounds(r, 0, 50)
		if offset < 0 || limit < 0 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "offset and limit are numbers")
		}
		return t.enterpriseReferencesRead(m, r.URL.Query().Get("element"), offset, limit, h.Now())
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/enterprise-query", Summary: "Elements of the enterprise model by stereotype, kind, words and day: the typed read", Answer: platform.EnterpriseQueryResult{}, Query: []transport.Param{{Name: "stereotype", Description: "UAF stereotypes, comma-separated; empty: all"}, {Name: "kind", Description: "Exact kind within the stereotype, e.g. line, workshop"}, {Name: "q", Description: "Words to find in id, name, short name or kind"}, {Name: "alive", Description: "\"all\" takes closed and future elements too; a YYYY-MM-DD day reads that day; empty: today"}, {Name: "limit", Description: "Elements to return (default 200, at most 1000)"}}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (platform.EnterpriseQueryResult, *kernel.Error) {
		if err := t.Admits(m); err != nil {
			return platform.EnterpriseQueryResult{}, err
		}
		return enterprisemodel.QueryRead(enterpriseModel(t), r.URL.Query(), h.Now())
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/enterprise-resolve", Summary: "Named elements by id: stored references read back, closed elements included and marked", Answer: platform.EnterpriseQueryResult{}, Query: []transport.Param{{Name: "ids", Description: "Element ids, comma-separated (at most 100)"}}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (platform.EnterpriseQueryResult, *kernel.Error) {
		if err := t.Admits(m); err != nil {
			return platform.EnterpriseQueryResult{}, err
		}
		return enterprisemodel.ResolveRead(enterpriseModel(t), r.URL.Query(), h.Now())
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/context/{type}/{id}", Summary: "A record with its history, references, links, flows and tasks: the context graph", Answer: objects.ContextView{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (objects.ContextView, *kernel.Error) {
		return t.contextRead(&m, r.PathValue("type"), r.PathValue("id"), h.Now())
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/search", Summary: "Records of every type the caller may read, by words and by the types' names", Answer: []build.Hit{}, Query: []transport.Param{{Name: "q", Description: "What to find"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.searchRead(&m, r.URL.Query().Get("q"), h.Now()))
	})
	rt.handle(transport.Route{Pattern: "GET /v1/knowledge", Summary: "Passages of the knowledge the caller may read, best first", Answer: []platform.Passage{}, Query: []transport.Param{{Name: "q", Description: "What to find"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Knowledge(&m, "", r.URL.Query().Get("q"), 8, h.Now()))
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/transcripts", Summary: "Model calls in full, for administrators of the agent or AI app who may also read what the run read (#130)", Answer: []Transcript{}, Query: []transport.Param{{Name: "run", Description: "An agent run"}}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) ([]Transcript, *kernel.Error) {
		return t.TranscriptsFor(m, r.URL.Query().Get("run"), 50, h.Now())
	}))
	h.objectSetRoutes(rt)
	rt.recordRead(transport.Route{Pattern: "GET /v1/records/{type}/{id}/provenance", Summary: "Visible field sources for an accepted record", Answer: objects.RecordProvenance{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (objects.RecordProvenance, *kernel.Error) {
		return t.objectOwner(t.Projection).RecordProvenance(m, r.PathValue("type"), r.PathValue("id"), h.Now())
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/records/{type}/{id}/sections/{section}", Summary: "One independently authorized object view section: links, activity, files, work, history or comments", Answer: objects.RecordSectionView{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (objects.RecordSectionView, *kernel.Error) {
		view, err := t.objectOwner(t.Projection).RecordSection(m, r.PathValue("type"), r.PathValue("id"), r.PathValue("section"), h.Now())
		if err == nil {
			if activity, ok := t.i18n.TranslateMessages(view.Activity, t.Language(m, r)).([]any); ok {
				view.Activity = activity
			}
		}
		return view, err
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/records/{type}/{id}", Summary: "One authorized record; its object view sections are read independently", Answer: objects.RecordView{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (objects.RecordView, *kernel.Error) {
		view, err := t.recordOf(m, r.PathValue("type"), r.PathValue("id"), h.Now())
		if err == nil && r.PathValue("type") == build.ObjectType {
			objects.DraftView(&view, func(record any) (any, *kernel.Error) { return t.objectDraftRecord(m, record) })
		}
		return view, err
	}))
}

func (t *Tenant) objectDraftRecord(m platform.Member, record any) (any, *kernel.Error) {
	m = t.authorizationMember(m)
	reader, _ := t.App(build.ID).(build.ConstructionDraftReader)
	return build.MaskDraftObject(record, m, reader, func(canonical any) any {
		t.Projection.Lock()
		et := t.Projection.Type(build.ObjectType)
		t.Projection.Unlock()
		_, hidden := objects.ViewOf(m, et)
		return records.Mask(et.Info(), reflect.ValueOf(canonical), hidden)
	})
}

func (t *Tenant) objectDraftPage(m platform.Member, page RecordPage) (RecordPage, *kernel.Error) {
	return objects.DraftPage(page, func(record any) (any, *kernel.Error) { return t.objectDraftRecord(m, record) })
}
