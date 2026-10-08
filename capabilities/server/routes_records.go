package platformserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// routesRecords serves records, files, links, aggregates, search, knowledge and transcripts.
func (h *Host) routesRecords(rt *routes) {
	rt.handle(Route{Pattern: "POST /v1/files", Summary: "Upload a file's bytes (the body; query name; Content-Type); answers its SHA-256 to attach with files.file.attach (ADR-0028)",
		Query: []Param{{"name", "The file's name"}}, Body: []byte{}, Answer: Upload{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		up, status, err := t.Upload(m, r.URL.Query().Get("name"), r.Header.Get("Content-Type"), r.Body, h.Now())
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		WriteJSON(w, http.StatusOK, up)
	})
	rt.handle(Route{Pattern: "GET /v1/files/{id}", Summary: "Download a file attached to a record the caller may read (ADR-0028)"}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		t.Download(w, m, r.PathValue("id"), h.Now())
	})
	rt.handle(Route{Pattern: "GET /v1/chain/{type}/{id}", Summary: "The chain an agent run or a flow instance belongs to: its record, flows, runs and the effects they caused (ADR-0029)", Answer: Chain{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		chain, err := t.ChainOf(m, r.PathValue("type")+"/"+r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, chain)
	})
	rt.handle(Route{Pattern: "POST /v1/import/{type}", Summary: "Import records from CSV: a header of field names with an id column; each row is the type's generated create or edit as the caller (ADR-0028)",
		Query: []Param{{"preview", "true: check each row and apply none"}}, Body: []byte{}, Answer: []ImportRow{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		rows, err := t.Import(m, r.PathValue("type"), body, r.URL.Query().Get("preview") == "true", h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, rows)
	})
	rt.handle(Route{Pattern: "GET /v1/export/{type}", Summary: "The records a list shows the caller, as CSV (ADR-0028)",
		Query: []Param{{"domain", "Filters in the prefix form"}, {"search", "Words to find"}, {"sort", "Fields, comma-separated"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		q := platform.Query{Domain: json.RawMessage(r.URL.Query().Get("domain")), Search: r.URL.Query().Get("search")}
		if sort := r.URL.Query().Get("sort"); sort != "" {
			q.Sort = strings.Split(sort, ",")
		}
		out, err := t.Export(m, r.PathValue("type"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename="+r.PathValue("type")+".csv")
		w.Write(out)
	})
	rt.handle(Route{Pattern: "POST /v1/records/{type}/query", Summary: "Read a bounded complete record-set expression in the caller's scope before paging (ADR-0046)", Body: platform.Query{}, Answer: RecordPage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
		page, err := t.Records(m, r.PathValue("type"), *q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	rt.handle(Route{Pattern: "POST /v1/link-types/{app}/{name}/{version}/{direction}/{id}", Summary: "Read a retained relationship using a bounded original query (ADR-0046)", Body: platform.Query{}, Answer: RecordPage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var q *platform.Query
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&q) != nil || q == nil || decoder.Decode(new(any)) != io.EOF {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A link query must be a bounded JSON object"))
			return
		}
		result, err := t.TraverseLink(m, platform.AssetBinding{Ref: platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetLinkType, Name: r.PathValue("name")}, SourceVersion: r.PathValue("version")}, r.PathValue("direction"), r.PathValue("id"), *q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	})
	rt.handle(Route{Pattern: "GET /v1/link-types/{app}/{name}/{version}/{direction}/{id}", Summary: "Follow one retained link type within the member's original record scope (ADR-0046)", Answer: RecordPage{}, Query: []Param{{"domain", "Additional record filters"}, {"search", "Words to find"}, {"sort", "Fields, comma-separated"}, {"offset", "Records to skip"}, {"limit", "Records in the page"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		q := platform.Query{Domain: json.RawMessage(r.URL.Query().Get("domain")), Search: r.URL.Query().Get("search")}
		if sort := r.URL.Query().Get("sort"); sort != "" {
			q.Sort = strings.Split(sort, ",")
		}
		fmt.Sscan(r.URL.Query().Get("offset"), &q.Offset)
		fmt.Sscan(r.URL.Query().Get("limit"), &q.Limit)
		result, err := t.TraverseLink(m, platform.AssetBinding{Ref: platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetLinkType, Name: r.PathValue("name")}, SourceVersion: r.PathValue("version")}, r.PathValue("direction"), r.PathValue("id"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	})
	rt.handle(Route{Pattern: "GET /v1/records/{type}", Summary: "A page of an entity type's records within the caller's scope", Answer: RecordPage{}, Query: []Param{{"domain", "Filters in the prefix form, JSON: [[\"stage\",\"=\",\"open\"]]"}, {"search", "Words to find"}, {"sort", "Fields, comma-separated; -field for descending"}, {"offset", "Records to skip"}, {"limit", "Records in the page"}, {"archived", "true: archived records too"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		q := platform.Query{Domain: json.RawMessage(r.URL.Query().Get("domain")), Search: r.URL.Query().Get("search"), Archived: r.URL.Query().Get("archived") == "true"}
		if sort := r.URL.Query().Get("sort"); sort != "" {
			q.Sort = strings.Split(sort, ",")
		}
		fmt.Sscan(r.URL.Query().Get("offset"), &q.Offset)
		fmt.Sscan(r.URL.Query().Get("limit"), &q.Limit)
		page, err := t.Records(m, r.PathValue("type"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	rt.handle(Route{Pattern: "GET /v1/enterprise-references", Summary: "Records that name an enterprise element, at any nesting depth, by entity type and page (ADR-0085 D4)", Answer: []EnterpriseReferenceGroup{}, Query: []Param{{"element", "The element's id"}, {"offset", "Records to skip, by column"}, {"limit", "Records per column (default 50, at most 200)"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		offset, limit := pageBounds(r, 0, 50)
		if offset < 0 || limit < 0 {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "offset and limit are numbers"))
			return
		}
		groups, err := t.EnterpriseReferences(m, r.URL.Query().Get("element"), offset, limit, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, groups)
	})
	rt.handle(Route{Pattern: "GET /v1/context/{type}/{id}", Summary: "A record with its history, references, links, flows and tasks: the context graph (ADR-0021)", Answer: ContextView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		view, err := t.Context(&m, r.PathValue("type"), r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, view)
	})
	rt.handle(Route{Pattern: "GET /v1/search", Summary: "Records of every type the caller may read, by words and by the types' names", Answer: []Hit{}, Query: []Param{{"q", "What to find"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Search(&m, r.URL.Query().Get("q"), h.Now()))
	})
	rt.handle(Route{Pattern: "GET /v1/knowledge", Summary: "Passages of the knowledge the caller may read, best first (ADR-0022)", Answer: []Passage{}, Query: []Param{{"q", "What to find"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.Knowledge(&m, "", r.URL.Query().Get("q"), 8, h.Now()))
	})
	rt.handle(Route{Pattern: "GET /v1/transcripts", Summary: "Model calls in full, for administrators of the agent or AI app who may also read what the run read (#130)", Answer: []Transcript{}, Query: []Param{{"run", "An agent run"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.TranscriptsFor(m, r.URL.Query().Get("run"), 50, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.handle(Route{Pattern: "POST /v1/aggregates/{type}/query", Summary: "Aggregate authorized membership or explicitly declared recent business-time windows (ADR-0046)", Body: AggregateQuery{}, Answer: Aggregate{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
		out, err := t.Aggregate(m, r.PathValue("type"), *q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.handle(Route{Pattern: "GET /v1/aggregates/{type}", Summary: "Groups and measures of an entity type's records within the caller's scope (ADR-0019)", Answer: Aggregate{}, Query: []Param{{"group", "Fields or field:month, comma-separated"}, {"measure", "count, sum:field, avg:field, min:field, max:field"}, {"domain", "Filters, JSON"}, {"search", "Words to find"}, {"archived", "true: archived records too"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		p := r.URL.Query()
		q := AggregateQuery{Domain: json.RawMessage(p.Get("domain")), Search: p.Get("search"), Archived: p.Get("archived") == "true"}
		if g := p.Get("group"); g != "" {
			q.Groups = strings.Split(g, ",")
		}
		if m := p.Get("measure"); m != "" {
			q.Measures = strings.Split(m, ",")
		}
		out, err := t.Aggregate(m, r.PathValue("type"), q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.handle(Route{Pattern: "GET /v1/records/{type}/{id}", Summary: "A record with its history and related records", Answer: RecordView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		view, err := t.RecordOf(m, r.PathValue("type"), r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		if activity, ok := t.i18n.TranslateMessages(view.Activity, t.Language(m, r)).([]any); ok {
			view.Activity = activity
		}
		WriteJSON(w, http.StatusOK, view)
	})
}
