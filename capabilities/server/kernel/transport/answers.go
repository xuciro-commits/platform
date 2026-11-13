package transport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

var statuses = map[pb.ErrorCode]int{
	pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT:     http.StatusBadRequest,
	pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA:       http.StatusBadRequest,
	pb.ErrorCode_ERROR_CODE_NOT_FOUND:            http.StatusNotFound,
	pb.ErrorCode_ERROR_CODE_POLICY_DENIED:        http.StatusForbidden,
	pb.ErrorCode_ERROR_CODE_NOT_AUTHORITY:        http.StatusMisdirectedRequest,
	pb.ErrorCode_ERROR_CODE_CONFLICT:             http.StatusConflict,
	pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT: http.StatusConflict,
	pb.ErrorCode_ERROR_CODE_REDIRECT_CYCLE:       http.StatusConflict,
	pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE:    http.StatusUnprocessableEntity,
}

func Reply(w http.ResponseWriter, record *pb.ChangeRecord, err *kernel.Error, issues ...platform.FieldIssue) {
	switch {
	case err != nil:
		body := platform.ErrorBody{Code: err.Error(), Message: err.Message, Issues: issues}
		if len(issues) > 0 && issues[0].Constraint != "" {
			body.Code = "CONSTRAINT_VIOLATION"
			body.Message = issues[0].Constraint + ": " + issues[0].Message
		}
		status := statuses[err.Code]
		if status == 0 {
			status = http.StatusInternalServerError
		}
		WriteJSON(w, status, map[string]any{"error": body})
	case record == nil:
		WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		raw, _ := protojson.Marshal(record)
		WriteJSON(w, http.StatusOK, map[string]json.RawMessage{"record": raw})
	}
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func Answer(w http.ResponseWriter, answer any, refusal *kernel.Error) {
	if refusal != nil {
		Reply(w, nil, refusal)
		return
	}
	WriteJSON(w, http.StatusOK, answer)
}

func AnswerOr(w http.ResponseWriter, failure int, answer any, err error) {
	if err != nil {
		WriteJSON(w, failure, map[string]string{"error": err.Error()})
		return
	}
	WriteJSON(w, http.StatusOK, answer)
}

func DecodeLimited(w http.ResponseWriter, r *http.Request, into any, limit int64) bool {
	if err := DecodeBodyLimit(r, into, limit); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		return false
	}
	return true
}

func PageBounds(r *http.Request, offsetDefault, limitDefault int) (int, int) {
	offset, limit := offsetDefault, limitDefault
	if value := r.URL.Query().Get("offset"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return -1, -1
		}
		offset = parsed
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return -1, -1
		}
		limit = parsed
	}
	return offset, limit
}

func NamedReadInputs(app platform.App, name string, query url.Values) (json.RawMessage, *kernel.Error) {
	if app == nil {
		return nil, nil
	}
	fields, declared := app.Manifest().ReadInputs[name]
	if !declared {
		return nil, nil
	}
	values := map[string]any{}
	for _, field := range fields {
		if !query.Has(field.Name) {
			continue
		}
		text := query.Get(field.Name)
		var value any = text
		var err error
		switch field.Type {
		case "integer":
			value, err = strconv.ParseInt(text, 10, 64)
		case "number":
			value, err = strconv.ParseFloat(text, 64)
		case "boolean":
			value, err = strconv.ParseBool(text)
		}
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, fmt.Sprintf("Read input %s must be %s", field.Name, field.Type))
		}
		values[field.Name] = value
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Read inputs cannot be encoded as JSON")
	}
	return raw, nil
}

func DecodeValueBody(w http.ResponseWriter, r *http.Request, into any, limit int) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, int64(limit)+1))
	if err != nil || len(raw) > limit {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return false
	}
	if _, err = platform.DecodeValue(raw, limit); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(into) != nil || decoder.Decode(new(any)) != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		return false
	}
	return true
}

func DecodeCommand(w http.ResponseWriter, r *http.Request, into any, limit int, valid func() bool, message string) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, int64(limit)+1))
	if err != nil || len(raw) > limit {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(into) != nil || !valid() {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": message})
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		return false
	}
	return true
}

type InboxTask struct {
	platform.WorkTask
	AnswerTitles []string `json:"answerTitles,omitempty"`
}

type MeTenant struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type SignIn struct {
	Issuer     string                   `json:"issuer,omitempty"`
	Client     string                   `json:"client,omitempty"`
	Identities []platform.LoginIdentity `json:"identities,omitempty"`
}

func ApplicationRunFilterOf(query url.Values) (platform.ApplicationRunFilter, *kernel.Error) {
	filter := platform.ApplicationRunFilter{Kind: query.Get("kind"), State: query.Get("state"), Subject: query.Get("subject")}
	for key, target := range map[string]*time.Time{"since": &filter.Since, "until": &filter.Until} {
		if value := query.Get(key); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return filter, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a valid application run time range")
			}
			*target = parsed
		}
	}
	return filter, nil
}
