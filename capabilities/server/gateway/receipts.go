package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

type receipt struct {
	Connection string                          `json:"connection"`
	Revision   string                          `json:"revision"`
	Hash       string                          `json:"hash"`
	Dispatch   platform.DriverDispatchIdentity `json:"dispatch"`
	At         time.Time                       `json:"at"`
	Answer     platform.DriverExecution        `json:"answer"`
}

func receiptID(dispatch platform.DriverDispatchIdentity) string {
	digest := sha256.Sum256([]byte(dispatch.RunID + "\x00" + dispatch.AttemptID))
	return hex.EncodeToString(digest[:])
}

func (s *Server) receiptPath(id string) string {
	return filepath.Join(s.config.StateDir, "receipts", id+".json")
}

// persistReceipt acknowledges a technical attempt only after its claim or
// answer survives a process restart. Raw samples are never written here.
func (s *Server) persistReceipt(id string, record receipt) error {
	return writeState(s.receiptPath(id), record)
}

func writeState(path string, value any) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".receipt-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(raw)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err == nil {
		var dir *os.File
		dir, err = os.Open(directory)
		if err == nil {
			err = dir.Sync()
			_ = dir.Close()
		}
	}
	return err
}

func (s *Server) loadReceipt(id string) (receipt, bool, error) {
	if existing, ok := s.receipts[id]; ok {
		return existing, true, nil
	}
	raw, err := os.ReadFile(s.receiptPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return receipt{}, false, nil
	}
	if err != nil {
		return receipt{}, false, err
	}
	var record receipt
	if len(raw) > 64<<10 || json.Unmarshal(raw, &record) != nil || receiptID(record.Dispatch) != id {
		return receipt{}, false, fmt.Errorf("technical attempt receipt is invalid")
	}
	s.receipts[id] = record
	return record, true, nil
}

func (s *Server) execute(w http.ResponseWriter, r *http.Request, connection, revision string, live *session) {
	var input platform.DriverExecute
	if err := decode(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Session = ""
	if !identity(input.Dispatch.RunID) || !identity(input.Dispatch.AttemptID) || input.Dispatch.BusinessKey == "" || len(input.Dispatch.BusinessKey) > 500 || input.Dispatch.BindingRevision != revision || strings.TrimPrefix(input.Dispatch.PackageDigest, "sha256:") != strings.TrimPrefix(live.binding.Digest, "sha256:") {
		writeError(w, http.StatusBadRequest, "execute needs the frozen run, attempt, business key, binding revision and package digest")
		return
	}
	raw, err := json.Marshal(struct {
		Connection string                 `json:"connection"`
		Request    platform.DriverExecute `json:"request"`
	}{connection, input})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(raw) > 32<<10 {
		writeError(w, http.StatusBadRequest, "command exceeds its byte budget")
		return
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	id := receiptID(input.Dispatch)
	s.mu.Lock()
	old, found, err := s.loadReceipt(id)
	if err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if found {
		s.mu.Unlock()
		if old.Hash != hash {
			writeError(w, http.StatusConflict, "this attempt identity already belongs to another command")
			return
		}
		writeJSON(w, http.StatusOK, old.Answer)
		return
	}
	if s.active[connection] != revision {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "this binding revision is read-only")
		return
	}
	if input.Dispatch.Deadline.IsZero() || !time.Now().Before(input.Dispatch.Deadline) {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "the dispatch deadline has expired")
		return
	}
	if !slices.Contains(live.profile.Operations, "execute") {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "this protocol profile is read-only")
		return
	}
	command, ok := findCommand(live.binding.Configuration, input.Command.Name)
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, "command is not declared by this binding")
		return
	}
	if err = checkCommandValue(command, input.Command.Value); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	record := receipt{Connection: connection, Revision: revision, Hash: hash, Dispatch: input.Dispatch, At: time.Now().UTC(), Answer: platform.DriverExecution{State: "unknown", Sending: "possible", Reason: "The attempt started; no durable device answer is available"}}
	if err = s.persistReceipt(id, record); err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusServiceUnavailable, "the attempt could not be saved; no command was sent")
		return
	}
	s.receipts[id] = record
	s.mu.Unlock()

	input.Session = live.open.Session
	ctx, cancel := context.WithDeadline(r.Context(), input.Dispatch.Deadline)
	defer cancel()
	var answer platform.DriverExecution
	if err = s.call(ctx, live, "execute", input, &answer); err != nil {
		answer = platform.DriverExecution{State: "unknown", Sending: "possible", Reason: "The driver did not provide a durable answer: " + err.Error()}
	} else if answer.Sending == "answered" {
		// A protocol answer is acceptance, never proof of business completion.
		answer.State = "accepted"
	} else if answer.Sending == "refused" || answer.Sending == "not-sent" {
		answer.State = "failed"
	} else {
		answer.State, answer.Sending = "unknown", "possible"
	}
	record.Answer = answer
	s.mu.Lock()
	if err = s.persistReceipt(id, record); err == nil {
		s.receipts[id] = record
	} else {
		answer = s.receipts[id].Answer
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, answer)
}

func findCommand(config platform.DriverConfiguration, name string) (platform.DriverCommand, bool) {
	for _, command := range config.Commands {
		if command.ID == name {
			return command, true
		}
	}
	return platform.DriverCommand{}, false
}
func checkCommandValue(command platform.DriverCommand, value any) error {
	if command.Min == "" && command.Max == "" {
		return nil
	}
	number := new(big.Rat)
	if _, ok := number.SetString(fmt.Sprint(value)); !ok {
		return fmt.Errorf("this command requires a numeric value")
	}
	for _, bound := range []struct {
		value   string
		minimum bool
	}{{command.Min, true}, {command.Max, false}} {
		if bound.value == "" {
			continue
		}
		limit, ok := new(big.Rat).SetString(bound.value)
		if !ok {
			return fmt.Errorf("command limits are invalid")
		}
		comparison := number.Cmp(limit)
		if bound.minimum && comparison < 0 || !bound.minimum && comparison > 0 {
			return fmt.Errorf("command value is outside its declared range")
		}
	}
	return nil
}

func (s *Server) commandSource(w http.ResponseWriter, connection, revision string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(s.config.StateDir, "receipts"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusServiceUnavailable, "technical receipts are unavailable")
		return
	}
	rows := []map[string]any{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		record, found, err := s.loadReceipt(id)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if !found || record.Connection != connection || record.Revision != revision {
			continue
		}
		rows = append(rows, map[string]any{"id": record.Dispatch.BusinessKey, "businessKey": record.Dispatch.BusinessKey, "runID": record.Dispatch.RunID, "attemptID": record.Dispatch.AttemptID, "at": record.At, "state": record.Answer.State, "reason": record.Answer.Reason})
	}
	writeJSON(w, http.StatusOK, rows)
}
