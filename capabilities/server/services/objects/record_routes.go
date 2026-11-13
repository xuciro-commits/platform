package objects

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"platformkernel/kernel"
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

func LocalizeImportRows(rows []platform.ImportRow, localize func([]platform.FieldIssue) []platform.FieldIssue) {
	for i := range rows {
		if rows[i].Error != nil {
			rows[i].Error.Issues = localize(rows[i].Error.Issues)
			if len(rows[i].Error.Issues) > 0 {
				rows[i].Error.Message = rows[i].Error.Issues[0].Message
			}
		}
	}
}

func DraftPage(page RecordPage, draft func(any) (any, *kernel.Error)) (RecordPage, *kernel.Error) {
	for index, record := range page.Records {
		canonical, err := draft(record)
		if err != nil {
			return page, err
		}
		page.Records[index] = canonical
	}
	return page, nil
}

func DraftView(view *RecordView, draft func(any) (any, *kernel.Error)) {
	if record, refusal := draft(view.Record); refusal == nil {
		view.Record = record
	} else {
		if view.ValueErrors == nil {
			view.ValueErrors = map[string]string{}
		}
		view.ValueErrors["actions"] = refusal.Message
	}
}
