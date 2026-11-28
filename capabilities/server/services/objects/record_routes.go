package objects

import (
	"encoding/json"
	"fmt"
	"net/url"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/services/ontology"
	"strings"

	"platformserver/platform"
)

func QueryFromValues(values url.Values) platform.Query {
	q := platform.Query{Domain: json.RawMessage(values.Get("domain")), Search: values.Get("search"), Archived: values.Get("archived") == "true"}
	if sort := values.Get("sort"); sort != "" {
		q.Sort = strings.Split(sort, ",")
	}
	return q
}

func PagedQueryFromValues(values url.Values) platform.Query {
	q := QueryFromValues(values)
	fmt.Sscan(values.Get("offset"), &q.Offset)
	fmt.Sscan(values.Get("limit"), &q.Limit)
	return q
}

func LocalizeImportRows(rows []protocol.ImportRow, localize func([]platform.FieldIssue) []platform.FieldIssue) {
	for i := range rows {
		if rows[i].Error != nil {
			rows[i].Error.Issues = localize(rows[i].Error.Issues)
			if len(rows[i].Error.Issues) > 0 {
				rows[i].Error.Message = rows[i].Error.Issues[0].Message
			}
		}
	}
}

func DraftPage(page RecordPage, draft func(any) (any, *platform.Error)) (RecordPage, *platform.Error) {
	for index, record := range page.Records {
		canonical, err := draft(record)
		if err != nil {
			return page, err
		}
		page.Records[index] = canonical
	}
	return page, nil
}

func DraftView(view *RecordView, draft func(any) (any, *platform.Error)) *platform.Error {
	if record, refusal := draft(view.Record); refusal == nil {
		view.Record = record
		changes, err := ontology.DraftChanges(record)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Published object definition is unavailable")
		}
		view.Changes = changes
	} else {
		if view.ValueErrors == nil {
			view.ValueErrors = map[string]string{}
		}
		view.ValueErrors["actions"] = refusal.Message
	}
	return nil
}

func LocalizeDeliveries(list []RecordDelivery, say func(string) string) []RecordDelivery {
	if len(list) == 0 {
		return list
	}
	out := make([]RecordDelivery, len(list))
	copy(out, list)
	for i := range out {
		out[i].Message = say(out[i].Message)
	}
	return out
}

func LocalizeSection(view *RecordSectionView, lang string, say func(string, string) string, translateMessages func(any, string) any) {
	view.Deliveries = LocalizeDeliveries(view.Deliveries, func(s string) string { return say(lang, s) })
	if activity, ok := translateMessages(view.Activity, lang).([]any); ok {
		view.Activity = activity
	}
}
