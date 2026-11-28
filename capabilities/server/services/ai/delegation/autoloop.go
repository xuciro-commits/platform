package delegation

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
)

type autoloopInboundEvent struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Subject *string `json:"subject,omitempty"`
	Summary *string `json:"summary,omitempty"`
	Detail  *string `json:"detail,omitempty"`
	Link    *string `json:"link,omitempty"`
	Hops    int     `json:"hops,omitempty"`
}

type autoloopAsk struct {
	Title     string   `json:"title"`
	Text      string   `json:"text"`
	Options   []string `json:"options"`
	Recommend *int     `json:"recommend,omitempty"`
	About     string   `json:"about,omitempty"`
	Key       string   `json:"key,omitempty"`
}

type autoloopNotice struct {
	Title string `json:"title"`
	Text  string `json:"text"`
	Key   string `json:"key,omitempty"`
	FYI   bool   `json:"fyi,omitempty"`
}

type autoloopReceipt struct {
	Outcome      string                 `json:"outcome"`
	Summary      string                 `json:"summary,omitempty"`
	Error        string                 `json:"error,omitempty"`
	LimitedUntil string                 `json:"limitedUntil,omitempty"`
	Session      string                 `json:"session,omitempty"`
	Usage        map[string]float64     `json:"usage,omitempty"`
	Ask          []autoloopAsk          `json:"ask,omitempty"`
	Notice       []autoloopNotice       `json:"notice,omitempty"`
	Resolve      []map[string]any       `json:"resolve,omitempty"`
	Later        []map[string]any       `json:"later,omitempty"`
	Events       []autoloopInboundEvent `json:"events,omitempty"`
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if tok, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return strings.TrimSpace(tok)
	}
	return ""
}

func writeAutoloopJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAutoloopErr(w http.ResponseWriter, status int, msg string) {
	writeAutoloopJSON(w, status, map[string]string{"error": msg})
}

func agentUse(agent string) string {
	lower := strings.ToLower(strings.TrimSpace(agent))
	if strings.Contains(lower, "codex") {
		return "codex"
	}
	return "claude"
}

func (a *Service) askFirstActions(d Delegation) []string {
	var out []string
	for _, schema := range d.Actions {
		for _, svc := range a.host.Apps() {
			if act, ok := svc.Manifest().Actions.Action(schema); ok && act.Ask {
				out = append(out, cmp.Or(act.Title, act.Schema))
				break
			}
		}
	}
	if len(out) == 0 {
		return []string{"external side effects"}
	}
	return out
}

func (a *Service) buildJobPayload(r *http.Request, job DelegationJob, dlg Delegation) map[string]any {
	c := a.automation(runtimeprotocol.AgentApp, false)
	events := make([]map[string]any, 0, len(job.EventIDs))
	for _, evID := range job.EventIDs {
		if ev, ok := platform.Get[DelegationEvent](c, evID); ok {
			events = append(events, map[string]any{
				"id":         ev.ExternalID,
				"source":     cmp.Or(ev.Source, "platform"),
				"sourceName": cmp.Or(ev.SourceName, "platform"),
				"type":       ev.EventType,
				"subject":    ev.Subject,
				"summary":    ev.Summary,
				"detail":     ev.Detail,
				"link":       ev.Link,
				"receivedAt": ev.ReceivedAt.UTC().Format(time.RFC3339),
				"hops":       ev.Hops,
			})
		}
	}
	answers := job.Answers
	if answers == nil {
		answers = []DelegationAnswer{}
	}
	mcpTok := a.MintRunMCPToken(job.Run, job.Attempt)
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	mcpURL := scheme + "://" + cmp.Or(r.Host, "127.0.0.1:8495") + "/mcp"
	var sessionVal any
	if job.Session != "" {
		sessionVal = job.Session
	}
	agentObj := map[string]any{
		"id":     dlg.Agent,
		"name":   dlg.Title,
		"use":    agentUse(dlg.Agent),
		"access": "full",
	}
	if dlg.Model != "" {
		agentObj["model"] = dlg.Model
	}
	if dlg.Program != "" {
		agentObj["program"] = dlg.Program
	}
	return map[string]any{
		"run":      job.Run,
		"token":    job.Token,
		"mcpToken": mcpTok,
		"attempt":  job.Attempt,
		"rule": map[string]any{
			"name":   dlg.Name,
			"cwd":    cmp.Or(dlg.Cwd, "/tmp"),
			"prompt": dlg.Prompt,
			"title":  dlg.Title,
		},
		"agent":    agentObj,
		"session":  sessionVal,
		"askFirst": a.askFirstActions(dlg),
		"context": map[string]any{
			"events":   events,
			"answers":  answers,
			"timers":   []any{},
			"waiting":  []any{},
			"open":     []any{},
			"decided":  []any{},
			"mcpToken": mcpTok,
			"mcpUrl":   mcpURL,
			"tenant":   a.tenant,
		},
	}
}

func (a *Service) cancelledRuns(runningParam string) []int {
	out := []int{}
	if strings.TrimSpace(runningParam) == "" {
		return out
	}
	c := a.automation(runtimeprotocol.AgentApp, false)
	for _, part := range strings.Split(runningParam, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 {
			continue
		}
		if runRec, ok := platform.Get[runtimeprotocol.AgentRunRecord](c, runRecordID(n)); ok && runRec.State == "stopped" {
			out = append(out, n)
		}
	}
	return out
}

func (a *Service) ServeAutoloop(w http.ResponseWriter, r *http.Request, now func() time.Time, memberOf func(*http.Request) (platform.Member, bool), submit func(platform.Member, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error)) {
	prefix := "/autoloop/" + a.tenant
	subpath := strings.TrimPrefix(r.URL.Path, prefix)
	serviceActor := platform.Member{
		ID:     "app:" + runtimeprotocol.AgentApp,
		Tenant: a.tenant,
		Roles:  map[string]string{runtimeprotocol.AgentApp: runtimeprotocol.AgentAdmin},
	}

	switch {
	case r.Method == http.MethodGet && subpath == "/v1/work":
		workerID, ok := a.verifyWorkerToken(bearerToken(r))
		if !ok {
			writeAutoloopErr(w, http.StatusUnauthorized, "invalid worker key")
			return
		}
		claim := cmp.Or(strings.TrimSpace(r.URL.Query().Get("claim")), "claim-"+workerID)
		waitSec, _ := strconv.Atoi(r.URL.Query().Get("wait"))
		if waitSec > 2 {
			waitSec = 2
		}
		hasCap := r.URL.Query().Get("capacity") != "0"
		runningParam := r.URL.Query().Get("running")
		deadline := time.Now().Add(time.Duration(waitSec) * time.Second)

		for {
			cancelList := a.cancelledRuns(runningParam)
			if !hasCap {
				writeAutoloopJSON(w, http.StatusOK, map[string]any{"job": nil, "cancel": cancelList})
				return
			}
			c := a.automation(runtimeprotocol.AgentApp, false)
			jobs, _, _ := platform.Find[DelegationJob](c, platform.Query{Sort: []string{"id"}})
			slices.SortFunc(jobs, func(x, y DelegationJob) int { return x.Run - y.Run })
			var picked *DelegationJob
			for i := range jobs {
				if jobs[i].State == "claimed" && jobs[i].ClaimKey == claim {
					picked = &jobs[i]
					break
				}
				if jobs[i].State == "queued" && picked == nil {
					picked = &jobs[i]
				}
			}
			if picked != nil {
				if picked.State == "queued" {
					sub := &pb.Submission{
						TenantId:       a.tenant,
						PrincipalId:    serviceActor.ID,
						Authority:      runtimeprotocol.AgentApp,
						IdempotencyKey: fmt.Sprintf("claim:%d:%s", picked.Run, claim),
						Target:         &pb.EntityRef{Type: runtimeprotocol.DelegationJobType, Id: picked.ID},
						Schema:         &pb.SchemaRef{Name: runtimeprotocol.SchemaDelegationClaim, Version: 1},
						Payload:        platform.Raw(map[string]any{"claim": claim}),
					}
					if _, refusal := submit(serviceActor, sub, now()); refusal != nil {
						writeAutoloopErr(w, http.StatusConflict, refusal.Message)
						return
					}
					c2 := a.automation(runtimeprotocol.AgentApp, false)
					updated, exists := platform.Get[DelegationJob](c2, picked.ID)
					if !exists || updated.ClaimKey != claim {
						writeAutoloopJSON(w, http.StatusOK, map[string]any{"job": nil, "cancel": cancelList})
						return
					}
					picked = &updated
				}
				c3 := a.automation(runtimeprotocol.AgentApp, false)
				dlg, exists := platform.Get[Delegation](c3, picked.DelegationID)
				if !exists {
					writeAutoloopJSON(w, http.StatusOK, map[string]any{"job": nil, "cancel": cancelList})
					return
				}
				writeAutoloopJSON(w, http.StatusOK, map[string]any{
					"job":    a.buildJobPayload(r, *picked, dlg),
					"cancel": cancelList,
				})
				return
			}
			if waitSec <= 0 || !time.Now().Before(deadline) {
				writeAutoloopJSON(w, http.StatusOK, map[string]any{"job": nil, "cancel": cancelList})
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(80 * time.Millisecond):
			}
		}

	case r.Method == http.MethodPost && strings.HasPrefix(subpath, "/v1/runs/") && strings.HasSuffix(subpath, "/started"):
		runStr := strings.TrimSuffix(strings.TrimPrefix(subpath, "/v1/runs/"), "/started")
		runNum, err := strconv.Atoi(runStr)
		if err != nil || runNum < 1 {
			writeAutoloopErr(w, http.StatusBadRequest, "invalid run id")
			return
		}
		tok := bearerToken(r)
		c := a.automation(runtimeprotocol.AgentApp, false)
		job, exists := platform.Get[DelegationJob](c, jobRecordID(runNum))
		if !exists || tok == "" || job.Token != tok {
			writeAutoloopErr(w, http.StatusUnauthorized, "invalid run token")
			return
		}
		var body struct {
			Session string `json:"session"`
			Link    string `json:"link"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sub := &pb.Submission{
			TenantId:       a.tenant,
			PrincipalId:    serviceActor.ID,
			Authority:      runtimeprotocol.AgentApp,
			IdempotencyKey: fmt.Sprintf("started:%s:%s", tok, body.Session),
			Target:         &pb.EntityRef{Type: runtimeprotocol.DelegationJobType, Id: job.ID},
			Schema:         &pb.SchemaRef{Name: runtimeprotocol.SchemaDelegationStarted, Version: 1},
			Payload:        platform.Raw(map[string]any{"token": tok, "session": body.Session, "link": body.Link}),
		}
		if _, refusal := submit(serviceActor, sub, now()); refusal != nil {
			writeAutoloopErr(w, http.StatusConflict, refusal.Message)
			return
		}
		writeAutoloopJSON(w, http.StatusOK, map[string]any{"ok": true})

	case r.Method == http.MethodPost && strings.HasPrefix(subpath, "/v1/runs/") && strings.HasSuffix(subpath, "/result"):
		runStr := strings.TrimSuffix(strings.TrimPrefix(subpath, "/v1/runs/"), "/result")
		runNum, err := strconv.Atoi(runStr)
		if err != nil || runNum < 1 {
			writeAutoloopErr(w, http.StatusBadRequest, "invalid run id")
			return
		}
		tok := bearerToken(r)
		c := a.automation(runtimeprotocol.AgentApp, false)
		job, exists := platform.Get[DelegationJob](c, jobRecordID(runNum))
		if !exists || tok == "" || job.Token != tok {
			writeAutoloopErr(w, http.StatusUnauthorized, "invalid run token")
			return
		}
		rawBody, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeAutoloopErr(w, http.StatusBadRequest, "cannot read receipt")
			return
		}
		var receipt autoloopReceipt
		if json.Unmarshal(rawBody, &receipt) != nil || !slices.Contains([]string{"done", "nothing", "failed", "cancelled"}, receipt.Outcome) {
			writeAutoloopErr(w, http.StatusBadRequest, "invalid receipt outcome")
			return
		}
		var canonicalMap any
		_ = json.Unmarshal(rawBody, &canonicalMap)
		digest, err := platform.CanonicalDigest(canonicalMap)
		if err != nil {
			writeAutoloopErr(w, http.StatusBadRequest, "invalid receipt json")
			return
		}
		if job.ReceiptDigest != "" {
			if job.ReceiptDigest == digest {
				writeAutoloopJSON(w, http.StatusOK, map[string]any{"rejected": []any{}, "duplicate": true})
				return
			}
			writeAutoloopErr(w, http.StatusConflict, "a different receipt was already recorded for this run token")
			return
		}
		sub := &pb.Submission{
			TenantId:       a.tenant,
			PrincipalId:    serviceActor.ID,
			Authority:      runtimeprotocol.AgentApp,
			IdempotencyKey: "receipt:" + tok,
			Target:         &pb.EntityRef{Type: runtimeprotocol.DelegationJobType, Id: job.ID},
			Schema:         &pb.SchemaRef{Name: runtimeprotocol.SchemaDelegationReceipt, Version: 1},
			Payload:        platform.Raw(map[string]any{"token": tok, "digest": digest, "receipt": receipt}),
		}
		if _, refusal := submit(serviceActor, sub, now()); refusal != nil {
			if refusal.Code == pb.ErrorCode_ERROR_CODE_CONFLICT {
				writeAutoloopErr(w, http.StatusConflict, refusal.Message)
				return
			}
			writeAutoloopErr(w, http.StatusBadRequest, refusal.Message)
			return
		}
		writeAutoloopJSON(w, http.StatusOK, map[string]any{"rejected": []any{}, "duplicate": false})

	case r.Method == http.MethodPost && subpath == "/v1/events":
		if _, ok := a.verifyWorkerToken(bearerToken(r)); !ok {
			writeAutoloopErr(w, http.StatusUnauthorized, "invalid worker key")
			return
		}
		var ev autoloopInboundEvent
		if json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&ev) != nil || strings.TrimSpace(ev.ID) == "" || strings.TrimSpace(ev.Type) == "" {
			writeAutoloopErr(w, http.StatusBadRequest, "event requires id and type")
			return
		}
		c := a.automation(runtimeprotocol.AgentApp, false)
		existingEvents, _, _ := platform.Find[DelegationEvent](c, platform.Query{})
		for _, prior := range existingEvents {
			if prior.ExternalID == ev.ID && prior.Source == "autoloop" {
				out := map[string]any{"event": prior.Seq, "duplicate": true, "routed": nonNilStrings(prior.Routed), "paused": nonNilStrings(prior.Paused)}
				if prior.HopBlocked {
					out["hopBlocked"] = true
				}
				writeAutoloopJSON(w, http.StatusOK, out)
				return
			}
		}
		evRecID := eventRecordID(a.tenant, ev.ID)
		sub := &pb.Submission{
			TenantId:       a.tenant,
			PrincipalId:    serviceActor.ID,
			Authority:      runtimeprotocol.AgentApp,
			IdempotencyKey: "autoloop-event:" + ev.ID,
			Target:         &pb.EntityRef{Type: runtimeprotocol.DelegationEventType, Id: evRecID},
			Schema:         &pb.SchemaRef{Name: runtimeprotocol.SchemaDelegationIngest, Version: 1},
			Payload:        platform.Raw(map[string]any{"event": ev}),
		}
		if _, refusal := submit(serviceActor, sub, now()); refusal != nil {
			writeAutoloopErr(w, http.StatusBadRequest, refusal.Message)
			return
		}
		cAfter := a.automation(runtimeprotocol.AgentApp, false)
		stored, _ := platform.Get[DelegationEvent](cAfter, evRecID)
		out := map[string]any{
			"event":     stored.Seq,
			"duplicate": false,
			"routed":    nonNilStrings(stored.Routed),
			"paused":    nonNilStrings(stored.Paused),
		}
		if stored.HopBlocked {
			out["hopBlocked"] = true
		}
		writeAutoloopJSON(w, http.StatusOK, out)

	case r.Method == http.MethodPost && subpath == "/v1/worker-token":
		m, ok := memberOf(r)
		if !ok || (!m.Holds(runtimeprotocol.AgentApp, runtimeprotocol.AgentAdmin) && !m.Holds("platform", "admin")) {
			writeAutoloopErr(w, http.StatusForbidden, "admin required")
			return
		}
		var body struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		tok := a.MintWorkerToken(body.ID)
		if tok == "" {
			writeAutoloopErr(w, http.StatusNotFound, "worker not found")
			return
		}
		writeAutoloopJSON(w, http.StatusOK, map[string]string{"token": tok})

	default:
		writeAutoloopErr(w, http.StatusNotFound, "not found")
	}
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (a *Service) receiveClaim(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	if !c.Automation && !c.Replaying {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	ac := a.host.Automation(c, runtimeprotocol.AgentApp)
	id := s.GetTarget().GetId()
	return a.authority.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		var p struct {
			Claim string `json:"claim"`
		}
		if json.Unmarshal(s.GetPayload(), &p) != nil || p.Claim == "" {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		job, exists := platform.Get[DelegationJob](ac, id)
		if !exists {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if job.State == "claimed" && job.ClaimKey == p.Claim {
			return func(*pb.ChangeRecord) {}, nil
		}
		if job.State != "queued" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Delegation job is no longer queued")
		}
		dlg, found := platform.Get[Delegation](ac, job.DelegationID)
		if !found || dlg.State != "active" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Delegation is paused")
		}
		if !c.Replaying {
			if _, ok, reason := a.checkDelegatorPermits(dlg); !ok {
				return func(r *pb.ChangeRecord) {
					a.pauseDelegationForPermission(c, r, dlg, reason, now)
					job.State = "cancelled"
					ac.Put(r, job)
				}, nil
			}
		}
		job.State = "claimed"
		job.Attempt++
		job.ClaimKey = p.Claim
		job.Token = a.mintAttemptToken(job.Run, job.Attempt)
		return func(r *pb.ChangeRecord) { ac.Put(r, job) }, nil
	})
}

func (a *Service) receiveStarted(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	if !c.Automation && !c.Replaying {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	ac := a.host.Automation(c, runtimeprotocol.AgentApp)
	id := s.GetTarget().GetId()
	return a.authority.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		var p struct {
			Token   string `json:"token"`
			Session string `json:"session"`
			Link    string `json:"link"`
		}
		if json.Unmarshal(s.GetPayload(), &p) != nil {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		job, exists := platform.Get[DelegationJob](ac, id)
		if !exists || job.Token != p.Token {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		if p.Session != "" {
			job.Session = p.Session
		}
		if p.Link != "" {
			job.Link = p.Link
		}
		return func(r *pb.ChangeRecord) { ac.Put(r, job) }, nil
	})
}

func (a *Service) receiveReceipt(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	if !c.Automation && !c.Replaying {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	ac := a.host.Automation(c, runtimeprotocol.AgentApp)
	id := s.GetTarget().GetId()
	return a.authority.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		var p struct {
			Token   string          `json:"token"`
			Digest  string          `json:"digest"`
			Receipt autoloopReceipt `json:"receipt"`
		}
		if json.Unmarshal(s.GetPayload(), &p) != nil || p.Token == "" || p.Digest == "" {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		job, exists := platform.Get[DelegationJob](ac, id)
		if !exists || job.Token != p.Token {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		if job.ReceiptDigest != "" {
			if job.ReceiptDigest == p.Digest {
				return func(*pb.ChangeRecord) {}, nil
			}
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Receipt content differs for this run token")
		}
		dlg, _ := platform.Get[Delegation](ac, job.DelegationID)
		runRec, hasRun := platform.Get[runtimeprotocol.AgentRunRecord](ac, job.RunRecordID)

		inTok := int(p.Receipt.Usage["input"])
		outTok := int(p.Receipt.Usage["output"])
		cost := p.Receipt.Usage["cost"]
		totalTok := inTok + outTok

		job.ReceiptDigest = p.Digest
		job.State = p.Receipt.Outcome
		job.Summary = p.Receipt.Summary
		job.ErrorText = p.Receipt.Error
		job.TokensUsed += totalTok
		if p.Receipt.Session != "" {
			job.Session = p.Receipt.Session
		}

		return func(r *pb.ChangeRecord) {
			ac.Put(r, job)
			if hasRun {
				runRec.StepsUsed++
				runRec.TokensUsed += totalTok
				runRec.Cost += cost
				runRec.Steps = append(runRec.Steps, runtimeprotocol.RunStep{
					At:      now,
					Tool:    "autoloop.receipt",
					Outcome: cmp.Or(p.Receipt.Summary, p.Receipt.Error, p.Receipt.Outcome),
					Tokens:  totalTok,
				})
				switch p.Receipt.Outcome {
				case "done", "nothing":
					if len(p.Receipt.Ask) > 0 {
						runRec.State = "waiting"
					} else {
						runRec.State = "done"
					}
					runRec.Result = p.Receipt.Summary
				case "cancelled":
					runRec.State = "stopped"
					runRec.Stopped = "stopped by worker"
				default:
					runRec.State = "stopped"
					runRec.Stopped = cmp.Or(p.Receipt.Error, "delegation run failed")
				}
				ac.Put(r, runRec)
			}

			if len(p.Receipt.Ask) > 0 && a.host != nil {
				if workSvc := a.host.App(runtimeprotocol.WorkApp); workSvc != nil {
					switch owner := workSvc.(type) {
					case runtimeprotocol.ApprovalOwner:
						for idx, askItem := range p.Receipt.Ask {
							recIdx := 0
							if askItem.Recommend != nil {
								recIdx = *askItem.Recommend
							}
							qid := fmt.Sprintf("dlgq-%d-%d", job.Run, idx+1)
							qReq := &pb.Submission{
								TenantId:       a.tenant,
								PrincipalId:    ac.ID,
								Authority:      runtimeprotocol.WorkApp,
								IdempotencyKey: "dlg-ask:" + qid,
								Target:         &pb.EntityRef{Type: runtimeprotocol.WorkQuestionType, Id: qid},
								Schema:         &pb.SchemaRef{Name: runtimeprotocol.WorkQuestionRequestSchema, Version: 1},
								Payload: platform.Raw(map[string]any{
									"requester":   dlg.OnBehalf,
									"title":       askItem.Title,
									"reason":      askItem.Text,
									"options":     askItem.Options,
									"recommend":   recIdx,
									"about":       askItem.About,
									"key":         cmp.Or(job.Session, askItem.Key),
									"delegation":  dlg.ID,
									"agentCaller": true,
									"caller": &runtimeprotocol.AICaller{
										Kind:    "delegation",
										Name:    dlg.Name,
										Version: dlg.Version,
										Model:   cmp.Or(dlg.Model, "external"),
										Run:     job.RunRecordID,
										Hops:    job.Hops,
									},
								}),
							}
							_, _ = owner.RequestQuestion(a.host.Automation(c, runtimeprotocol.WorkApp), qReq, now)
						}
					}
				}
			}

			if dlg.OnBehalf != "" && dlg.Notify != "never" {
				for idx, n := range p.Receipt.Notice {
					ac.Notify(platform.Notification{
						Category: runtimeprotocol.NotificationSystem,
						Title:    n.Title,
						Body:     n.Text,
						Ref:      runtimeprotocol.RunType + "/" + job.RunRecordID,
						Key:      fmt.Sprintf("dlg-notice:%d:%d", job.Run, idx+1),
					}, now, platform.Recipient{Member: dlg.OnBehalf})
				}
				if dlg.Notify == "always" && (p.Receipt.Outcome == "done" || p.Receipt.Outcome == "nothing") && p.Receipt.Summary != "" {
					ac.Notify(platform.Notification{
						Category: runtimeprotocol.NotificationSystem,
						Title:    dlg.Title,
						Body:     p.Receipt.Summary,
						Ref:      runtimeprotocol.RunType + "/" + job.RunRecordID,
						Key:      fmt.Sprintf("dlg-done:%d", job.Run),
					}, now, platform.Recipient{Member: dlg.OnBehalf})
				}
				if p.Receipt.Outcome == "failed" {
					ac.Notify(platform.Notification{
						Category: runtimeprotocol.NotificationSystem,
						Title:    "Delegation failed: " + dlg.Title,
						Body:     cmp.Or(p.Receipt.Error, "Run failed"),
						Ref:      runtimeprotocol.RunType + "/" + job.RunRecordID,
						Key:      fmt.Sprintf("dlg-failed:%d", job.Run),
					}, now, platform.Recipient{Member: dlg.OnBehalf})
				}
			}

			for idx, emitted := range p.Receipt.Events {
				seq := a.nextEventSeq(ac)
				ev := DelegationEvent{
					Record:     platform.Record{ID: fmt.Sprintf("ev-%d", seq)},
					Seq:        seq,
					ExternalID: cmp.Or(emitted.ID, fmt.Sprintf("emit-%d-%d", job.Run, idx+1)),
					Source:     "delegation." + dlg.Name,
					SourceName: dlg.Title,
					EventType:  emitted.Type,
					Subject:    derefStr(emitted.Subject),
					Summary:    derefStr(emitted.Summary),
					Detail:     "资料:\n" + strconv.Quote(derefStr(emitted.Detail)),
					Link:       derefStr(emitted.Link),
					Hops:       job.Hops + 1,
					ReceivedAt: now,
				}
				routed, paused, hopBlocked := a.routeInboundEvent(c, r, ev, now)
				ev.Routed, ev.Paused, ev.HopBlocked = routed, paused, hopBlocked
				ac.Put(r, ev)
			}
		}, nil
	})
}

func (a *Service) receiveIngest(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	if !c.Automation && !c.Replaying {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	ac := a.host.Automation(c, runtimeprotocol.AgentApp)
	id := s.GetTarget().GetId()
	return a.authority.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		var p struct {
			Event autoloopInboundEvent `json:"event"`
		}
		if json.Unmarshal(s.GetPayload(), &p) != nil || strings.TrimSpace(p.Event.ID) == "" {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		if existing, exists := platform.Get[DelegationEvent](ac, id); exists {
			_ = existing
			return func(*pb.ChangeRecord) {}, nil
		}
		return func(r *pb.ChangeRecord) {
			seq := a.nextEventSeq(ac)
			detail := derefStr(p.Event.Detail)
			if detail != "" && !strings.HasPrefix(detail, "资料:") {
				detail = "资料:\n" + detail
			}
			ev := DelegationEvent{
				Record:     platform.Record{ID: id},
				Seq:        seq,
				ExternalID: p.Event.ID,
				Source:     "autoloop",
				SourceName: "autoloop",
				EventType:  p.Event.Type,
				Subject:    derefStr(p.Event.Subject),
				Summary:    derefStr(p.Event.Summary),
				Detail:     detail,
				Link:       derefStr(p.Event.Link),
				Hops:       max(0, p.Event.Hops),
				ReceivedAt: now,
			}
			routed, paused, hopBlocked := a.routeInboundEvent(c, r, ev, now)
			ev.Routed, ev.Paused, ev.HopBlocked = routed, paused, hopBlocked
			ac.Put(r, ev)
		}, nil
	})
}

func (a *Service) routeInboundEvent(c platform.Caller, r *pb.ChangeRecord, ev DelegationEvent, now time.Time) ([]string, []string, bool) {
	ac := a.host.Automation(c, runtimeprotocol.AgentApp)
	delegations, _, _ := platform.Find[Delegation](ac, platform.Query{Sort: []string{"id"}})
	var routed, paused []string
	anyHopBlocked := false
	for _, d := range delegations {
		matches := ev.EventType == "delegation."+d.Name || ev.EventType == d.Name ||
			(d.Trigger.Object != "" && strings.HasPrefix(ev.Subject, d.Trigger.Object+"/"))
		if !matches {
			continue
		}
		didRoute, didPause, hopBlocked := a.enqueueDelegationEvent(c, r, d, ev, nil, "", now)
		if didRoute {
			routed = append(routed, d.Name)
		}
		if didPause {
			paused = append(paused, d.Name)
		}
		if hopBlocked {
			anyHopBlocked = true
		}
	}
	return routed, paused, anyHopBlocked
}
