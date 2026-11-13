package host

import transport "platformserver/kernel/transport"

import (
	"encoding/json"
	"io"
	"net/http"
	"platformserver/kernel/records"
	pageowner "platformserver/services/apps/pages"
	automation "platformserver/services/automate/runtime"
	"platformserver/services/objects"
	"reflect"
	"strconv"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/services/resources/authoring"
	enterprisemodel "platformserver/services/security/organization"

	"platformserver/platform"
)

func enterpriseModel(w http.ResponseWriter, t *Tenant) (platform.EnterpriseQueries, bool) {
	app, _ := t.App(enterprisemodel.ID).(platform.EnterpriseQueries)
	if app == nil {
		Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This tenant has no enterprise model"))
	}
	return app, app != nil
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
	rt.recordRead(transport.Route{Pattern: "GET /v1/page-model/rules", Summary: "List original action criteria available to the builder model inspector", Answer: []pageowner.PageModelRule{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		c := platform.NewCaller(runtime{t: t}, m, "build", false, false)
		if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/page-model/rules", "read", []string{"builder"}, true)); err != nil {
			Reply(w, nil, err)
			return
		}
		values := []pageowner.PageModelRule{}
		for _, entry := range t.definitions {
			if entry.Action == nil {
				continue
			}
			if criteria, ok := t.modelCriteria(entry.Ref); ok {
				values = append(values, pageowner.PageModelRule{Action: entry.Ref, Title: entry.Action.Title, Criteria: *criteria})
			}
		}
		WriteJSON(w, http.StatusOK, values)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/record-sets/{type}/{id}/{name}", Summary: "Read a bounded declared collection through its original owner", Answer: platform.RecordSetResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		info, ok := t.Entity(r.PathValue("type"))
		if !ok {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Collection object is unavailable"))
			return
		}
		caller := platform.NewCaller(runtime{t: t}, m, info.App, false, false)
		value, err := caller.ReadRecordSet(info.Type, r.PathValue("id"), r.PathValue("name"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, value)
	})
	rt.recordRead(transport.Route{Pattern: "POST /v1/relation-migrations/preview", Summary: "Preview an explicit migration of historical links into their endpoint owner's records", Body: platform.AssetBinding{}, Answer: platform.RelationMigrationPlan{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var binding platform.AssetBinding
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&binding) != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid relationship binding"))
			return
		}
		caller := platform.NewCaller(runtime{t: t}, m, binding.Ref.App, false, false)
		plan, err := t.relationMigrationPlan(t.Projection, caller, binding, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, plan)
	})
	rt.handle(transport.Route{Pattern: "POST /v1/files", Summary: "Upload a file's bytes (the body; query name; Content-Type); answers its SHA-256 to attach with files.file.attach",
		Query: []transport.Param{{Name: "name", Description: "The file's name"}}, Body: []byte{}, Answer: Upload{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
	rt.recordRead(transport.Route{Pattern: "GET /v1/chain/{type}/{id}", Summary: "The chain an agent run or a flow instance belongs to: its record, flows, runs and the effects they caused", Answer: automation.Chain{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		chain, err := t.chainRead(m, r.PathValue("type")+"/"+r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, chain)
	})
	rt.handle(transport.Route{Pattern: "POST /v1/import/{type}", Summary: "Import records from CSV: a header of field names with an id column; each row is the type's generated create or edit as the caller",
		Query: []transport.Param{{Name: "preview", Description: "true: check each row and apply none"}}, Body: []byte{}, Answer: []platform.ImportRow{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		rows, err := t.Import(m, r.PathValue("type"), body, r.URL.Query().Get("preview") == "true", h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		objects.LocalizeImportRows(rows, func(issues []platform.FieldIssue) []platform.FieldIssue {
			return t.localizedFieldIssues(m, issues, t.Language(m, r))
		})
		WriteJSON(w, http.StatusOK, rows)
	})
	exportCSV := func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant, q platform.Query) {
		out, err := t.exportRead(m, r.PathValue("type"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		transport.WriteCSV(w, r.PathValue("type"), out)
	}
	rt.recordRead(transport.Route{Pattern: "GET /v1/export/{type}", Summary: "The records a list shows the caller, as CSV",
		Query: []transport.Param{{Name: "domain", Description: "Filters in the prefix form"}, {Name: "search", Description: "Words to find"}, {Name: "sort", Description: "Fields, comma-separated"}, {Name: "archived", Description: "true: archived records too"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
	rt.recordRead(transport.Route{Pattern: "GET /v1/records/{type}", Summary: "A page of an entity type's records within the caller's scope", Answer: RecordPage{}, Query: []transport.Param{{Name: "domain", Description: "Filters in the prefix form, JSON: [[\"stage\",\"=\",\"open\"]]"}, {Name: "search", Description: "Words to find"}, {Name: "sort", Description: "Fields, comma-separated; -field for descending"}, {Name: "offset", Description: "Records to skip"}, {Name: "limit", Description: "Records in the page"}, {Name: "archived", Description: "true: archived records too"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		page, err := t.recordsRead(m, r.PathValue("type"), objects.PagedQueryFromValues(r.URL.Query()), h.Now())
		if err == nil && r.PathValue("type") == build.ObjectType {
			page, err = t.objectDraftPage(m, page)
		}
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/enterprise-usage", Summary: "Uses preventing removal or closure of an enterprise element", Answer: enterprisemodel.EnterpriseUsage{}, Query: []transport.Param{{Name: "element", Description: "The enterprise element"}, {Name: "until", Description: "Proposed closure day"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.enterpriseUsageRead(m, r.URL.Query().Get("element"), r.URL.Query().Get("until"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/enterprise-references", Summary: "Records that name an enterprise element, at any nesting depth, by entity type and page", Answer: []records.EnterpriseReferenceGroup{}, Query: []transport.Param{{Name: "element", Description: "The element's id"}, {Name: "offset", Description: "Records to skip, by column"}, {Name: "limit", Description: "Records per column (default 50, at most 200)"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		offset, limit := pageBounds(r, 0, 50)
		if offset < 0 || limit < 0 {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "offset and limit are numbers"))
			return
		}
		groups, err := t.enterpriseReferencesRead(m, r.URL.Query().Get("element"), offset, limit, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, groups)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/enterprise-query", Summary: "Elements of the enterprise model by stereotype, kind, words and day: the typed read", Answer: platform.EnterpriseQueryResult{}, Query: []transport.Param{{Name: "stereotype", Description: "UAF stereotypes, comma-separated; empty: all"}, {Name: "kind", Description: "Exact kind within the stereotype, e.g. line, workshop"}, {Name: "q", Description: "Words to find in id, name, short name or kind"}, {Name: "alive", Description: "\"all\" takes closed and future elements too; a YYYY-MM-DD day reads that day; empty: today"}, {Name: "limit", Description: "Elements to return (default 200, at most 1000)"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if err := t.Admits(m); err != nil {
			Reply(w, nil, err)
			return
		}
		limit := 200
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 1000 {
				Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "limit is a number between 1 and 1000"))
				return
			}
			limit = n
		}
		app, ok := enterpriseModel(w, t)
		if !ok {
			return
		}
		out, err := app.Query(strings.Split(r.URL.Query().Get("stereotype"), ","), r.URL.Query().Get("kind"), r.URL.Query().Get("q"),
			r.URL.Query().Get("alive"), limit, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/enterprise-resolve", Summary: "Named elements by id: stored references read back, closed elements included and marked", Answer: platform.EnterpriseQueryResult{}, Query: []transport.Param{{Name: "ids", Description: "Element ids, comma-separated (at most 100)"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if err := t.Admits(m); err != nil {
			Reply(w, nil, err)
			return
		}
		app, ok := enterpriseModel(w, t)
		if !ok {
			return
		}
		out, err := app.Resolve(strings.Split(r.URL.Query().Get("ids"), ","), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/context/{type}/{id}", Summary: "A record with its history, references, links, flows and tasks: the context graph", Answer: objects.ContextView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		view, err := t.contextRead(&m, r.PathValue("type"), r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, view)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/search", Summary: "Records of every type the caller may read, by words and by the types' names", Answer: []build.Hit{}, Query: []transport.Param{{Name: "q", Description: "What to find"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.searchRead(&m, r.URL.Query().Get("q"), h.Now()))
	})
	rt.handle(transport.Route{Pattern: "GET /v1/knowledge", Summary: "Passages of the knowledge the caller may read, best first", Answer: []platform.Passage{}, Query: []transport.Param{{Name: "q", Description: "What to find"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Knowledge(&m, "", r.URL.Query().Get("q"), 8, h.Now()))
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/transcripts", Summary: "Model calls in full, for administrators of the agent or AI app who may also read what the run read (#130)", Answer: []Transcript{}, Query: []transport.Param{{Name: "run", Description: "An agent run"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.TranscriptsFor(m, r.URL.Query().Get("run"), 50, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	h.objectSetRoutes(rt)
	rt.recordRead(transport.Route{Pattern: "GET /v1/records/{type}/{id}/provenance", Summary: "Visible field sources for an accepted record", Answer: RecordProvenance{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		provenance, err := t.objectOwner(t.Projection).RecordProvenance(m, r.PathValue("type"), r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, provenance)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/records/{type}/{id}/sections/{section}", Summary: "One independently authorized object view section: links, activity, files, work, history or comments", Answer: objects.RecordSectionView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		view, err := t.objectOwner(t.Projection).RecordSection(m, r.PathValue("type"), r.PathValue("id"), r.PathValue("section"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		if activity, ok := t.i18n.TranslateMessages(view.Activity, t.Language(m, r)).([]any); ok {
			view.Activity = activity
		}
		WriteJSON(w, http.StatusOK, view)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/records/{type}/{id}", Summary: "One authorized record; its object view sections are read independently", Answer: objects.RecordView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		view, err := t.recordOf(m, r.PathValue("type"), r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		if r.PathValue("type") == build.ObjectType {
			objects.DraftView(&view, func(record any) (any, *kernel.Error) { return t.objectDraftRecord(m, record) })
		}
		WriteJSON(w, http.StatusOK, view)
	})
}

func (t *Tenant) objectDraftRecord(m platform.Member, record any) (any, *kernel.Error) {
	object, ok := record.(build.Object)
	m = t.authorizationMember(m)
	if !ok || !m.Holds(build.ID, build.Builder) {
		return record, nil
	}
	owner, ok := t.App(build.ID).(build.ConstructionDraftReader)
	if !ok {
		return record, nil
	}
	canonical, err := owner.CanonicalObject(object)
	if err != nil {
		return record, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	t.Projection.Lock()
	et := t.Projection.Type(build.ObjectType)
	t.Projection.Unlock()
	_, hidden := viewOf(m, et)
	return records.Mask(et.Info(), reflect.ValueOf(canonical), hidden), nil
}

func (t *Tenant) objectDraftPage(m platform.Member, page RecordPage) (RecordPage, *kernel.Error) {
	return objects.DraftPage(page, func(record any) (any, *kernel.Error) { return t.objectDraftRecord(m, record) })
}
