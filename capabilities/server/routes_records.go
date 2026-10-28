package platformserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"platformserver/internal/records"
	"reflect"
	"strconv"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	enterprisemodel "platformserver/engine/organization"
	"platformserver/internal/construction"
	"platformserver/internal/host"
	"platformserver/platform"
)

// enterpriseApp is the tenant's enterprise modeling app, when one is mounted:
// the typed query and resolve reads dispatch into it.
func enterpriseApp(t *Tenant) host.EnterpriseQueries {
	d, _ := t.app(enterprisemodel.ID).(host.EnterpriseQueries)
	return d
}

// routesRecords serves records, files, links, aggregates, search, knowledge and transcripts.
func (h *Host) routesRecords(rt *routes) {
	rt.handle(Route{Pattern: "POST /v1/enterprise-links/{id}/preview", Summary: "Preview an authorized tenant source through its original ready connection without importing it", Answer: EnterpriseLinkPreview{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		preview, err := t.PreviewEnterpriseLink(r.Context(), m, r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, preview)
	})
	rt.handle(Route{Pattern: "POST /v1/page-model/read", Summary: "Read a published model binding or builder preview through the original record and rule owners", Body: PageModelRead{}, Answer: PageModelResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request *PageModelRead
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || request == nil || decoder.Decode(new(any)) != io.EOF {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A model read needs one bounded request"))
			return
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		value, err := t.readPageModel(m, *request, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		// Model fields are declarations; records, model bindings and permission
		// evidence retain their original values, even for a field named title.
		owner, _, _ := strings.Cut(value.Object, ".")
		WriteJSON(w, http.StatusOK, struct {
			PageModelResult
			Fields any `json:"fields"`
		}{value, t.i18n.translate(value.Fields, owner, t.Language(m, r))})
	})
	rt.recordRead(Route{Pattern: "GET /v1/page-model/rules", Summary: "List original action criteria available to the builder model inspector", Answer: []PageModelRule{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {

		c := platform.NewCaller(runtime{t}, m, "build", false, false)
		if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/page-model/rules", "read", []string{"builder"}, true)); err != nil {
			Reply(w, nil, err)
			return
		}
		values := []PageModelRule{}
		for _, entry := range t.definitions {
			if entry.Action == nil {
				continue
			}
			if criteria, ok := t.modelCriteria(entry.Ref); ok {
				values = append(values, PageModelRule{Action: entry.Ref, Title: entry.Action.Title, Criteria: *criteria})
			}
		}
		WriteJSON(w, http.StatusOK, values)
	})
	rt.recordRead(Route{Pattern: "GET /v1/record-sets/{type}/{id}/{name}", Summary: "Read a bounded declared collection through its original owner", Answer: platform.RecordSetResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {

		info, ok := t.entity(r.PathValue("type"))
		if !ok {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Collection object is unavailable"))
			return
		}
		caller := platform.NewCaller(runtime{t}, m, info.App, false, false)
		value, err := caller.ReadRecordSet(info.Type, r.PathValue("id"), r.PathValue("name"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, value)
	})
	rt.handle(Route{Pattern: "POST /v1/relation-migrations/preview", Summary: "Preview an explicit migration of historical links into their endpoint owner's records", Body: platform.AssetBinding{}, Answer: platform.RelationMigrationPlan{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var binding platform.AssetBinding
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&binding) != nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid relationship binding"))
			return
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		caller := platform.NewCaller(runtime{t}, m, binding.Ref.App, false, false)
		plan, err := t.relationMigrationPlan(t.records, caller, binding, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, plan)
	})
	rt.recordRead(Route{Pattern: "GET /v1/interfaces/{name}/records", Summary: "Common-field records of actual interface implementers, with original type and id", Answer: InterfaceRecordPage{}, Query: []Param{{"search", "Words to find"}, {"sort", "Common fields, comma-separated"}, {"offset", "Records to skip"}, {"limit", "Records in this window (at most 200)"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		offset, limit := pageBounds(r, 0, 25)
		query := platform.Query{Domain: json.RawMessage(r.URL.Query().Get("domain")), Search: r.URL.Query().Get("search"), Offset: offset, Limit: limit}
		if raw := r.URL.Query().Get("sort"); raw != "" {
			query.Sort = strings.Split(raw, ",")
		}
		out, err := t.interfaceRecordsRead(m, r.PathValue("name"), query, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.handle(Route{Pattern: "POST /v1/files", Summary: "Upload a file's bytes (the body; query name; Content-Type); answers its SHA-256 to attach with files.file.attach",
		Query: []Param{{"name", "The file's name"}}, Body: []byte{}, Answer: Upload{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		up, status, err := t.Upload(m, r.URL.Query().Get("name"), r.Header.Get("Content-Type"), r.Body, h.Now())
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		WriteJSON(w, http.StatusOK, up)
	})
	rt.handle(Route{Pattern: "GET /v1/files/{id}", Summary: "Download a file attached to a record the caller may read"}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		t.Download(w, m, r.PathValue("id"), h.Now())
	})
	rt.recordRead(Route{Pattern: "GET /v1/chain/{type}/{id}", Summary: "The chain an agent run or a flow instance belongs to: its record, flows, runs and the effects they caused", Answer: Chain{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		chain, err := t.chainRead(m, r.PathValue("type")+"/"+r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, chain)
	})
	rt.handle(Route{Pattern: "POST /v1/import/{type}", Summary: "Import records from CSV: a header of field names with an id column; each row is the type's generated create or edit as the caller",
		Query: []Param{{"preview", "true: check each row and apply none"}}, Body: []byte{}, Answer: []ImportRow{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		rows, err := t.Import(m, r.PathValue("type"), body, r.URL.Query().Get("preview") == "true", h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		for i := range rows {
			if rows[i].Error != nil {
				rows[i].Error.Issues = t.localizedFieldIssues(m, rows[i].Error.Issues, t.Language(m, r))
				if len(rows[i].Error.Issues) > 0 {
					rows[i].Error.Message = rows[i].Error.Issues[0].Message
				}
			}
		}
		WriteJSON(w, http.StatusOK, rows)
	})
	exportCSV := func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant, q platform.Query) {
		out, err := t.exportRead(m, r.PathValue("type"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename="+r.PathValue("type")+".csv")
		w.Write(out)
	}
	rt.recordRead(Route{Pattern: "GET /v1/export/{type}", Summary: "The records a list shows the caller, as CSV",
		Query: []Param{{"domain", "Filters in the prefix form"}, {"search", "Words to find"}, {"sort", "Fields, comma-separated"}, {"archived", "true: archived records too"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		q := platform.Query{Domain: json.RawMessage(r.URL.Query().Get("domain")), Search: r.URL.Query().Get("search"), Archived: r.URL.Query().Get("archived") == "true"}
		if sort := r.URL.Query().Get("sort"); sort != "" {
			q.Sort = strings.Split(sort, ",")
		}
		exportCSV(w, r, m, t, q)
	})
	rt.recordRead(Route{Pattern: "POST /v1/export/{type}", Summary: "Export the caller's complete authorized record query as CSV, including sets, relations and business time", Body: platform.Query{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, platform.QuerySetMaxBytes))
		decoder.DisallowUnknownFields()
		var q *platform.Query
		if decoder.Decode(&q) != nil || q == nil || q.Limit < 0 || q.Offset < 0 || decoder.Decode(new(any)) != io.EOF {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		exportCSV(w, r, m, t, *q)
	})
	rt.recordRead(Route{Pattern: "POST /v1/records/{type}/query", Summary: "Read a bounded complete record-set expression in the caller's scope before paging", Body: platform.Query{}, Answer: RecordPage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, platform.QuerySetMaxBytes))
		decoder.DisallowUnknownFields()
		var q *platform.Query
		if decoder.Decode(&q) != nil || q == nil || q.Limit < 0 || q.Offset < 0 {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		page, err := t.recordsRead(m, r.PathValue("type"), *q, h.Now())
		if err == nil && r.PathValue("type") == construction.ObjectType {
			page, err = t.objectDraftPage(m, page)
		}
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	rt.recordRead(Route{Pattern: "POST /v1/link-types/{app}/{name}/{version}/{direction}/{id}", Summary: "Read a retained relationship using a bounded original query", Body: platform.Query{}, Answer: RecordPage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var q *platform.Query
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&q) != nil || q == nil || decoder.Decode(new(any)) != io.EOF {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A link query must be a bounded JSON object"))
			return
		}
		result, err := t.traverseLinkRead(m, platform.AssetBinding{Ref: platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetLinkType, Name: r.PathValue("name")}, SourceVersion: r.PathValue("version")}, r.PathValue("direction"), r.PathValue("id"), *q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	})
	rt.recordRead(Route{Pattern: "GET /v1/link-types/{app}/{name}/{version}/{direction}/{id}", Summary: "Follow one retained link type within the member's original record scope", Answer: RecordPage{}, Query: []Param{{"domain", "Additional record filters"}, {"search", "Words to find"}, {"sort", "Fields, comma-separated"}, {"offset", "Records to skip"}, {"limit", "Records in the page"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		q := platform.Query{Domain: json.RawMessage(r.URL.Query().Get("domain")), Search: r.URL.Query().Get("search")}
		if sort := r.URL.Query().Get("sort"); sort != "" {
			q.Sort = strings.Split(sort, ",")
		}
		fmt.Sscan(r.URL.Query().Get("offset"), &q.Offset)
		fmt.Sscan(r.URL.Query().Get("limit"), &q.Limit)
		result, err := t.traverseLinkRead(m, platform.AssetBinding{Ref: platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetLinkType, Name: r.PathValue("name")}, SourceVersion: r.PathValue("version")}, r.PathValue("direction"), r.PathValue("id"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	})
	rt.recordRead(Route{Pattern: "GET /v1/records/{type}", Summary: "A page of an entity type's records within the caller's scope", Answer: RecordPage{}, Query: []Param{{"domain", "Filters in the prefix form, JSON: [[\"stage\",\"=\",\"open\"]]"}, {"search", "Words to find"}, {"sort", "Fields, comma-separated; -field for descending"}, {"offset", "Records to skip"}, {"limit", "Records in the page"}, {"archived", "true: archived records too"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		q := platform.Query{Domain: json.RawMessage(r.URL.Query().Get("domain")), Search: r.URL.Query().Get("search"), Archived: r.URL.Query().Get("archived") == "true"}
		if sort := r.URL.Query().Get("sort"); sort != "" {
			q.Sort = strings.Split(sort, ",")
		}
		fmt.Sscan(r.URL.Query().Get("offset"), &q.Offset)
		fmt.Sscan(r.URL.Query().Get("limit"), &q.Limit)
		page, err := t.recordsRead(m, r.PathValue("type"), q, h.Now())
		if err == nil && r.PathValue("type") == construction.ObjectType {
			page, err = t.objectDraftPage(m, page)
		}
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	rt.recordRead(Route{Pattern: "GET /v1/enterprise-usage", Summary: "Uses preventing removal or closure of an enterprise element", Answer: EnterpriseUsage{}, Query: []Param{{"element", "The enterprise element"}, {"until", "Proposed closure day"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {

		answer, err := t.enterpriseUsageRead(m, r.URL.Query().Get("element"), r.URL.Query().Get("until"), h.Now())

		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})

	rt.recordRead(Route{Pattern: "GET /v1/enterprise-references", Summary: "Records that name an enterprise element, at any nesting depth, by entity type and page", Answer: []EnterpriseReferenceGroup{}, Query: []Param{{"element", "The element's id"}, {"offset", "Records to skip, by column"}, {"limit", "Records per column (default 50, at most 200)"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
	rt.recordRead(Route{Pattern: "GET /v1/enterprise-query", Summary: "Elements of the enterprise model by stereotype, kind, words and day: the typed read", Answer: enterprisemodel.EnterpriseQueryResult{}, Query: []Param{{"stereotype", "UAF stereotypes, comma-separated; empty: all"}, {"kind", "Exact kind within the stereotype, e.g. line, workshop"}, {"q", "Words to find in id, name, short name or kind"}, {"alive", "\"all\" takes closed and future elements too; a YYYY-MM-DD day reads that day; empty: today"}, {"limit", "Elements to return (default 200, at most 1000)"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if err := t.admits(m); err != nil {
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
		app := enterpriseApp(t)
		if app == nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This tenant has no enterprise model"))
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
	rt.recordRead(Route{Pattern: "GET /v1/enterprise-resolve", Summary: "Named elements by id: stored references read back, closed elements included and marked", Answer: enterprisemodel.EnterpriseQueryResult{}, Query: []Param{{"ids", "Element ids, comma-separated (at most 100)"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if err := t.admits(m); err != nil {
			Reply(w, nil, err)
			return
		}
		app := enterpriseApp(t)
		if app == nil {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This tenant has no enterprise model"))
			return
		}
		out, err := app.Resolve(strings.Split(r.URL.Query().Get("ids"), ","), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.recordRead(Route{Pattern: "GET /v1/context/{type}/{id}", Summary: "A record with its history, references, links, flows and tasks: the context graph", Answer: ContextView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		view, err := t.contextRead(&m, r.PathValue("type"), r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, view)
	})
	rt.recordRead(Route{Pattern: "GET /v1/search", Summary: "Records of every type the caller may read, by words and by the types' names", Answer: []Hit{}, Query: []Param{{"q", "What to find"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.searchRead(&m, r.URL.Query().Get("q"), h.Now()))
	})
	rt.handle(Route{Pattern: "GET /v1/knowledge", Summary: "Passages of the knowledge the caller may read, best first", Answer: []Passage{}, Query: []Param{{"q", "What to find"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Knowledge(&m, "", r.URL.Query().Get("q"), 8, h.Now()))
	})
	rt.recordRead(Route{Pattern: "GET /v1/transcripts", Summary: "Model calls in full, for administrators of the agent or AI app who may also read what the run read (#130)", Answer: []Transcript{}, Query: []Param{{"run", "An agent run"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.TranscriptsFor(m, r.URL.Query().Get("run"), 50, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.recordRead(Route{Pattern: "POST /v1/aggregates/{type}/query", Summary: "Aggregate authorized membership or explicitly declared recent business-time windows", Body: AggregateQuery{}, Answer: Aggregate{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, platform.QuerySetMaxBytes))
		decoder.DisallowUnknownFields()
		var q *AggregateQuery
		if decoder.Decode(&q) != nil || q == nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		out, err := t.aggregateRead(m, r.PathValue("type"), *q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.recordRead(Route{Pattern: "GET /v1/aggregates/{type}", Summary: "Groups and measures of an entity type's records within the caller's scope", Answer: Aggregate{}, Query: []Param{{"group", "Fields or field:month, comma-separated"}, {"measure", "count, sum:field, avg:field, min:field, max:field"}, {"domain", "Filters, JSON"}, {"search", "Words to find"}, {"archived", "true: archived records too"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		p := r.URL.Query()
		q := AggregateQuery{Domain: json.RawMessage(p.Get("domain")), Search: p.Get("search"), Archived: p.Get("archived") == "true"}
		if g := p.Get("group"); g != "" {
			q.Groups = strings.Split(g, ",")
		}
		if m := p.Get("measure"); m != "" {
			q.Measures = strings.Split(m, ",")
		}
		out, err := t.aggregateRead(m, r.PathValue("type"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.recordRead(Route{Pattern: "GET /v1/records/{type}/{id}", Summary: "A record with its history and related records", Answer: RecordView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		view, err := t.recordOf(m, r.PathValue("type"), r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		if r.PathValue("type") == construction.ObjectType {
			if record, refusal := t.objectDraftRecord(m, view.Record); refusal == nil {
				view.Record = record
			} else {
				if view.ValueErrors == nil {
					view.ValueErrors = map[string]string{}
				}
				view.ValueErrors["actions"] = refusal.Message
			}
		}
		if activity, ok := t.i18n.TranslateMessages(view.Activity, t.Language(m, r)).([]any); ok {
			view.Activity = activity
		}
		WriteJSON(w, http.StatusOK, view)
	})
}

// The HTTP record and shared live routes use the same authoring projection.
// Internal record readers retain the accepted representation for execution.
func (t *Tenant) objectDraftRecord(m platform.Member, record any) (any, *kernel.Error) {
	object, ok := record.(construction.Object)
	m = t.authorizationMember(m)
	if !ok || !m.Holds(construction.ID, construction.Builder) {
		return record, nil
	}
	owner, ok := t.app(construction.ID).(host.ConstructionDraftReader)
	if !ok {
		return record, nil
	}
	canonical, err := owner.CanonicalObject(object)
	if err != nil {
		return record, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	// Conversion receives only visible fields, and cannot restore a hidden
	// destination field when replacing an older expression.
	t.records.mu.Lock()
	et := t.records.types[construction.ObjectType]
	t.records.mu.Unlock()
	_, hidden := viewOf(m, et)
	return records.Mask(et.info, reflect.ValueOf(canonical), hidden), nil
}

func (t *Tenant) objectDraftPage(m platform.Member, page RecordPage) (RecordPage, *kernel.Error) {
	for index, record := range page.Records {
		canonical, err := t.objectDraftRecord(m, record)
		if err != nil {
			return page, err
		}
		page.Records[index] = canonical
	}
	return page, nil
}
