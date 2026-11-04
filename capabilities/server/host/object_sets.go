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
	"platformserver/services/objects"
	"platformserver/services/resources/authoring"
	"time"
)

type ObjectSetRequest = objects.ObjectSetRequest
type ObjectSetCount = objects.ObjectSetCount
type objectWindow = objects.Window

func (t *Tenant) loadObjectSet(store *records.Store, c platform.Caller, set platform.ObjectSet, w objectWindow, now time.Time, bound ...platform.NamedQuery) (RecordPage, string, *kernel.Error) {
	return t.objectOwner(store).LoadObjectSet(store, c, set, w, now, bound...)
}

func (t *Tenant) countObjectSet(store *records.Store, c platform.Caller, set platform.ObjectSet, now time.Time) (ObjectSetCount, *kernel.Error) {
	return t.objectOwner(store).CountObjectSet(store, c, set, now)
}

func (t *Tenant) aggregateObjectSet(m platform.Member, req ObjectSetRequest, now time.Time) (records.Aggregate, *kernel.Error) {
	return t.objectOwner(t.records).AggregateObjectSet(m, req, now)
}

func (h *Host) objectSetRoutes(rt *routes) {
	decode := func(w http.ResponseWriter, r *http.Request, op string) (ObjectSetRequest, bool) {
		var req *ObjectSetRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, platform.QuerySetMaxBytes))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&req) != nil || req == nil || decoder.Decode(new(any)) != io.EOF || req.Offset < 0 || req.Limit < 0 || req.Set.Check() != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return ObjectSetRequest{}, false
		}
		paged := len(req.Sort) > 0 || req.Offset != 0 || req.Limit != 0
		aggregated := len(req.Groups) > 0 || len(req.Measures) > 0 || req.Window != nil || req.Histogram != nil || req.MaxRows != 0 || req.Collection != nil
		if op == "load" && aggregated || op == "aggregate" && (paged || req.Format) || op == "count" && (paged || aggregated || req.Format) {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The request carries options of another object set operation"))
			return ObjectSetRequest{}, false
		}
		return *req, true
	}
	rt.recordRead(transport.Route{Pattern: "POST /v1/object-sets/load", Summary: "Load a page of one composable object set in the caller's scope: sort, offset, limit and the complete visible total", Body: ObjectSetRequest{}, Answer: RecordPage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		req, ok := decode(w, r, "load")
		if !ok {
			return
		}
		page, object, err := t.loadObjectSet(t.records, t.objectCaller(m, ""), req.Set, objectWindow{Sort: req.Sort, Offset: req.Offset, Limit: req.Limit, Bounded: true, Format: req.Format}, h.Now())
		if err == nil && object == build.ObjectType {
			page, err = t.objectDraftPage(m, page)
		}
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, page)
	})
	rt.recordRead(transport.Route{Pattern: "POST /v1/object-sets/aggregate", Summary: "Aggregate one composable object set: groups and measures, a histogram or a recent-event window", Body: ObjectSetRequest{}, Answer: records.Aggregate{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		req, ok := decode(w, r, "aggregate")
		if !ok {
			return
		}
		out, err := t.aggregateObjectSet(m, req, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.recordRead(transport.Route{Pattern: "POST /v1/object-sets/count", Summary: "Count the records of one composable object set that the caller may read", Body: ObjectSetRequest{}, Answer: ObjectSetCount{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		req, ok := decode(w, r, "count")
		if !ok {
			return
		}
		out, err := t.countObjectSet(t.records, t.objectCaller(m, ""), req.Set, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
}
