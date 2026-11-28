package monitoring

import (
	"cmp"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

const (
	QuotaType          = protocol.MonitoringQuotaType
	SchemaQuotaSet     = "monitoring.quota.set"
	QuotaRecordID      = "current"
	BackupType         = protocol.MonitoringBackupType
	SchemaBackupRecord = "monitoring.backup.record"
	BackupRecordID     = "latest"

	ReasonRecordQuotaExceeded  = "Workspace record quota reached; remove records or upgrade the workspace plan"
	ReasonStorageQuotaExceeded = "Workspace storage quota reached; remove files or upgrade the workspace plan"
	ReasonWriteQuotaExceeded   = "Workspace daily write quota reached; try again tomorrow or upgrade the workspace plan"
	ReasonAIQuotaExceeded      = "Workspace AI quota reached; upgrade the workspace plan to continue"
	ReasonPackageQuotaExceeded = "Workspace package quota reached; retire unused packages or upgrade the workspace plan"
)

func DefaultQuotaPlans() []protocol.QuotaPlan {
	return []protocol.QuotaPlan{
		{
			ID: "starter", Title: "Starter", Description: "For small teams getting started",
			Quota: protocol.TenantQuota{
				Plan:              "starter",
				Records:           protocol.QuotaThresholds{Soft: 1000, Hard: 2000},
				StorageBytes:      protocol.QuotaThresholds{Soft: 100 << 20, Hard: 250 << 20},
				DailyWrites:       protocol.QuotaThresholds{Soft: 1000, Hard: 2500},
				AITokens:          protocol.QuotaThresholds{Soft: 100000, Hard: 250000},
				AICost:            protocol.AICostThresholds{Soft: 5, Hard: 10},
				InstalledPackages: protocol.QuotaThresholds{Soft: 2, Hard: 3},
			},
		},
		{
			ID: "team", Title: "Team", Description: "For growing workspaces with multiple apps",
			Quota: protocol.TenantQuota{
				Plan:              "team",
				Records:           protocol.QuotaThresholds{Soft: 25000, Hard: 50000},
				StorageBytes:      protocol.QuotaThresholds{Soft: 5 << 30, Hard: 10 << 30},
				DailyWrites:       protocol.QuotaThresholds{Soft: 20000, Hard: 50000},
				AITokens:          protocol.QuotaThresholds{Soft: 2000000, Hard: 5000000},
				AICost:            protocol.AICostThresholds{Soft: 50, Hard: 100},
				InstalledPackages: protocol.QuotaThresholds{Soft: 8, Hard: 12},
			},
		},
		{
			ID: "enterprise", Title: "Enterprise", Description: "For high-volume multi-department operations",
			Quota: protocol.TenantQuota{
				Plan:              "enterprise",
				Records:           protocol.QuotaThresholds{Soft: 500000, Hard: 1000000},
				StorageBytes:      protocol.QuotaThresholds{Soft: 100 << 30, Hard: 250 << 30},
				DailyWrites:       protocol.QuotaThresholds{Soft: 250000, Hard: 500000},
				AITokens:          protocol.QuotaThresholds{Soft: 25000000, Hard: 50000000},
				AICost:            protocol.AICostThresholds{Soft: 500, Hard: 1000},
				InstalledPackages: protocol.QuotaThresholds{Soft: 30, Hard: 50},
			},
		},
	}
}

func ResolveQuotaRequest(plans []protocol.QuotaPlan, req protocol.QuotaRequest) (protocol.TenantQuota, error) {
	var out protocol.TenantQuota
	planID := strings.TrimSpace(req.Plan)
	if planID != "" {
		i := slices.IndexFunc(plans, func(p protocol.QuotaPlan) bool { return p.ID == planID })
		if i < 0 {
			return out, fmt.Errorf("unknown quota plan %q", planID)
		}
		out = plans[i].Quota
		out.Plan = planID
	}
	if req.Quota != nil {
		if p := strings.TrimSpace(req.Quota.Plan); p != "" && out.Plan == "" {
			out.Plan = p
		}
		overlayThresholds(&out.Records, req.Quota.Records)
		overlayThresholds(&out.StorageBytes, req.Quota.StorageBytes)
		overlayThresholds(&out.DailyWrites, req.Quota.DailyWrites)
		overlayThresholds(&out.AITokens, req.Quota.AITokens)
		overlayThresholds(&out.InstalledPackages, req.Quota.InstalledPackages)
		if req.Quota.AICost.Soft != 0 || req.Quota.AICost.Hard != 0 {
			if req.Quota.AICost.Soft != 0 {
				out.AICost.Soft = req.Quota.AICost.Soft
			}
			if req.Quota.AICost.Hard != 0 {
				out.AICost.Hard = req.Quota.AICost.Hard
			}
		}
	}
	if err := validateQuota(out); err != nil {
		return out, err
	}
	return out, nil
}

func overlayThresholds(dst *protocol.QuotaThresholds, src protocol.QuotaThresholds) {
	if src.Soft != 0 {
		dst.Soft = src.Soft
	}
	if src.Hard != 0 {
		dst.Hard = src.Hard
	}
}

func validateQuota(q protocol.TenantQuota) error {
	for _, item := range []protocol.QuotaThresholds{q.Records, q.StorageBytes, q.DailyWrites, q.AITokens, q.InstalledPackages} {
		if item.Soft < 0 || item.Hard < 0 || (item.Soft > 0 && item.Hard > 0 && item.Soft > item.Hard) {
			return fmt.Errorf("quota thresholds must be nonnegative with soft <= hard")
		}
	}
	if q.AICost.Soft < 0 || q.AICost.Hard < 0 || (q.AICost.Soft > 0 && q.AICost.Hard > 0 && q.AICost.Soft > q.AICost.Hard) {
		return fmt.Errorf("AI cost quota thresholds must be nonnegative with soft <= hard")
	}
	return nil
}

func WorkspaceQuotaFrom(q protocol.TenantQuota, reason, by string) protocol.WorkspaceQuota {
	return protocol.WorkspaceQuota{
		Record:       platform.Record{ID: QuotaRecordID},
		Plan:         q.Plan,
		RecordsSoft:  q.Records.Soft,
		RecordsHard:  q.Records.Hard,
		StorageSoft:  q.StorageBytes.Soft,
		StorageHard:  q.StorageBytes.Hard,
		WritesSoft:   q.DailyWrites.Soft,
		WritesHard:   q.DailyWrites.Hard,
		AITokensSoft: q.AITokens.Soft,
		AITokensHard: q.AITokens.Hard,
		AICostSoft:   q.AICost.Soft,
		AICostHard:   q.AICost.Hard,
		PackagesSoft: q.InstalledPackages.Soft,
		PackagesHard: q.InstalledPackages.Hard,
		Reason:       reason,
		AssignedBy:   by,
	}
}

func TenantQuotaFrom(w protocol.WorkspaceQuota) protocol.TenantQuota {
	return protocol.TenantQuota{
		Plan:              w.Plan,
		Records:           protocol.QuotaThresholds{Soft: w.RecordsSoft, Hard: w.RecordsHard},
		StorageBytes:      protocol.QuotaThresholds{Soft: w.StorageSoft, Hard: w.StorageHard},
		DailyWrites:       protocol.QuotaThresholds{Soft: w.WritesSoft, Hard: w.WritesHard},
		AITokens:          protocol.QuotaThresholds{Soft: w.AITokensSoft, Hard: w.AITokensHard},
		AICost:            protocol.AICostThresholds{Soft: w.AICostSoft, Hard: w.AICostHard},
		InstalledPackages: protocol.QuotaThresholds{Soft: w.PackagesSoft, Hard: w.PackagesHard},
	}
}

func CurrentQuota(store *records.Store) *protocol.TenantQuota {
	if store == nil {
		return nil
	}
	rows := records.RowsOf[protocol.WorkspaceQuota](store, QuotaType)
	for _, row := range rows {
		if row.ID == QuotaRecordID && !row.Archived {
			q := TenantQuotaFrom(row)
			return &q
		}
	}
	return nil
}

func (m *Monitoring) decideQuota(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *platform.Error) {
	if !c.Automation && !c.Replaying {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	var payload struct {
		Quota      protocol.TenantQuota `json:"quota"`
		Reason     string               `json:"reason"`
		AssignedBy string               `json:"assignedBy"`
	}
	if json.Unmarshal(s.GetPayload(), &payload) != nil || validateQuota(payload.Quota) != nil {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	current, _ := platform.Get[protocol.WorkspaceQuota](c, QuotaRecordID)
	next := WorkspaceQuotaFrom(payload.Quota, strings.TrimSpace(payload.Reason), cmp.Or(payload.AssignedBy, c.ID))
	next.Record = current.Record
	next.ID = QuotaRecordID
	next.Archived = false
	if err := c.Check(next); err != nil {
		return nil, err
	}
	return func(r *pb.ChangeRecord) { c.Put(r, next) }, nil
}

func QuotaExemptSubmission(s *pb.Submission) bool {
	if s == nil {
		return true
	}
	schema := s.GetSchema().GetName()
	if schema == "" || schema == SchemaQuotaSet || schema == SchemaBackupRecord || schema == protocol.WorkQuestionRequestSchema || schema == protocol.WorkQuestionType+".answer" ||
		schema == "platform.member.join" || schema == "platform.notification.read" {
		return true
	}
	for _, suffix := range []string{".archive", ".remove", ".delete", ".discard", ".revoke", ".retire", ".uninstall", ".offboard", ".cancel"} {
		if strings.HasSuffix(schema, suffix) {
			return true
		}
	}
	return false
}

func CheckHardQuota(usage protocol.TenantUsage, quota *protocol.TenantQuota, s *pb.Submission, packages map[string]*protocol.InstalledPackage) *platform.Error {
	if quota == nil || QuotaExemptSubmission(s) {
		return nil
	}
	schema := s.GetSchema().GetName()
	aiScoped := schema == "input:"+protocol.AIReserveCall || s.GetAuthority() == protocol.AIApp || s.GetAuthority() == protocol.AgentApp ||
		strings.HasPrefix(schema, "ai.") || strings.HasPrefix(schema, "agent.")
	if aiScoped && ((quota.AITokens.Hard > 0 && usage.AITokens >= quota.AITokens.Hard) || (quota.AICost.Hard > 0 && usage.AICost >= quota.AICost.Hard)) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, ReasonAIQuotaExceeded)
	}
	if schema == "input:"+protocol.AIReserveCall {
		if quota.DailyWrites.Hard > 0 && usage.WritesToday >= quota.DailyWrites.Hard {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, ReasonWriteQuotaExceeded)
		}
		return nil
	}
	if quota.Records.Hard > 0 && usage.Records >= quota.Records.Hard {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, ReasonRecordQuotaExceeded)
	}
	if quota.StorageBytes.Hard > 0 && usage.StorageBytes >= quota.StorageBytes.Hard {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, ReasonStorageQuotaExceeded)
	}
	if quota.DailyWrites.Hard > 0 && usage.WritesToday >= quota.DailyWrites.Hard {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, ReasonWriteQuotaExceeded)
	}
	if quota.InstalledPackages.Hard > 0 && (schema == "platform.package.install" || schema == "platform.package.import") {
		targetID := s.GetTarget().GetId()
		if targetID == "" || packages[targetID] == nil || packages[targetID].State != "active" {
			if usage.InstalledPackages >= quota.InstalledPackages.Hard {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, ReasonPackageQuotaExceeded)
			}
		}
	}
	return nil
}

type SoftQuotaPrompt struct {
	ID     string
	Kind   string
	Day    string
	Title  string
	Reason string
}

func DueSoftQuotaQuestions(usage protocol.TenantUsage, quota *protocol.TenantQuota, exists func(id string) bool, now time.Time) []SoftQuotaPrompt {
	if quota == nil {
		return nil
	}
	day := now.UTC().Format(time.DateOnly)
	var out []SoftQuotaPrompt
	check := func(kind, title string, triggered bool, reason string) {
		if !triggered {
			return
		}
		id := "quota-" + kind + "-" + day
		if exists != nil && exists(id) {
			return
		}
		out = append(out, SoftQuotaPrompt{ID: id, Kind: kind, Day: day, Title: title, Reason: reason})
	}
	check("records", "Workspace record usage crossed the soft limit",
		quota.Records.Soft > 0 && usage.Records >= quota.Records.Soft,
		fmt.Sprintf("%d of %d records used; review retention or request a plan upgrade.", usage.Records, cmp.Or(quota.Records.Hard, quota.Records.Soft)))
	check("storage", "Workspace storage crossed the soft limit",
		quota.StorageBytes.Soft > 0 && usage.StorageBytes >= quota.StorageBytes.Soft,
		fmt.Sprintf("%d bytes used against a %d-byte soft limit.", usage.StorageBytes, quota.StorageBytes.Soft))
	check("writes", "Workspace daily writes crossed the soft limit",
		quota.DailyWrites.Soft > 0 && usage.WritesToday >= quota.DailyWrites.Soft,
		fmt.Sprintf("%d writes recorded today against a %d soft limit.", usage.WritesToday, quota.DailyWrites.Soft))
	check("ai", "Workspace AI usage crossed the soft limit",
		(quota.AITokens.Soft > 0 && usage.AITokens >= quota.AITokens.Soft) || (quota.AICost.Soft > 0 && usage.AICost >= quota.AICost.Soft),
		fmt.Sprintf("%d tokens ($%.4f) used; review model budgets or upgrade the plan.", usage.AITokens, usage.AICost))
	check("packages", "Workspace installed packages crossed the soft limit",
		quota.InstalledPackages.Soft > 0 && usage.InstalledPackages >= quota.InstalledPackages.Soft,
		fmt.Sprintf("%d packages active against a %d soft limit.", usage.InstalledPackages, quota.InstalledPackages.Soft))
	return out
}

func PlanOf(q *protocol.TenantQuota) string {
	if q == nil {
		return ""
	}
	return q.Plan
}

func InputQuotaSubmission(authority, name string, body []byte) *pb.Submission {
	if name == protocol.AIReserveCall {
		var in protocol.AIReserveInput
		if json.Unmarshal(body, &in) == nil && in.Cancel {
			return nil
		}
	}
	return &pb.Submission{Authority: authority, Schema: &pb.SchemaRef{Name: "input:" + name}}
}

func SoftQuotaSubmissions(tenant string, usage protocol.TenantUsage, store *records.Store, admins []string, now time.Time) (platform.Member, []*pb.Submission) {
	caller := platform.Member{ID: "app:" + protocol.WorkApp, Tenant: tenant, Roles: map[string]string{protocol.WorkApp: "admin"}}
	if len(admins) == 0 || store == nil {
		return caller, nil
	}
	quota := CurrentQuota(store)
	if quota == nil {
		return caller, nil
	}
	et := store.Type(protocol.WorkQuestionType)
	exists := func(id string) bool { return et != nil && et.Rows().Get(id) != nil }
	prompts := DueSoftQuotaQuestions(usage, quota, exists, now)
	if len(prompts) == 0 {
		return caller, nil
	}
	out := make([]*pb.Submission, 0, len(prompts))
	for _, p := range prompts {
		out = append(out, &pb.Submission{
			TenantId: tenant, PrincipalId: caller.ID, Authority: protocol.WorkApp,
			IdempotencyKey: "quota:" + p.Kind + ":" + p.Day,
			Target:         &pb.EntityRef{Type: protocol.WorkQuestionType, Id: p.ID},
			Schema:         &pb.SchemaRef{Name: protocol.WorkQuestionRequestSchema, Version: 1},
			Payload: platform.Raw(map[string]any{
				"requester": admins[0], "candidates": admins, "app": "platform",
				"title": p.Title, "reason": p.Reason,
				"options": []string{"acknowledge", "upgrade_plan"}, "optionTitles": []string{"Acknowledge", "Upgrade plan"},
				"recommend": 1, "key": p.ID,
			}),
		})
	}
	return caller, out
}

func PrepareHostQuota(tenant, subject string, req protocol.QuotaRequest, now time.Time) (platform.Member, *pb.Submission, protocol.AuditEntry, error) {
	resolved, err := ResolveQuotaRequest(DefaultQuotaPlans(), req)
	if err != nil {
		return platform.Member{}, nil, protocol.AuditEntry{}, err
	}
	actor := "host:" + subject
	caller := platform.Member{ID: "app:" + ID, Tenant: tenant}
	digest, _ := platform.CanonicalDigest([]any{tenant, actor, resolved.Plan, req.Reason, now.UTC().UnixNano()})
	if len(digest) > 24 {
		digest = digest[:24]
	}
	sub := &pb.Submission{
		TenantId: tenant, PrincipalId: caller.ID, Authority: ID,
		IdempotencyKey: "host-quota:" + digest,
		Target:         &pb.EntityRef{Type: QuotaType, Id: QuotaRecordID},
		Schema:         &pb.SchemaRef{Name: SchemaQuotaSet, Version: 1},
		Payload:        platform.Raw(map[string]any{"quota": resolved, "reason": req.Reason, "assignedBy": actor}),
	}
	target := tenant
	if resolved.Plan != "" {
		target += ":" + resolved.Plan
	}
	if req.Reason != "" {
		target += ":" + req.Reason
	}
	audit := protocol.AuditEntry{At: now.UTC(), Member: actor, App: "platform", Action: "host.quota.set", Target: target}
	return caller, sub, audit, nil
}

func CurrentBackup(store *records.Store) *protocol.BackupStatus {
	if store == nil {
		return nil
	}
	for _, row := range records.RowsOf[protocol.WorkspaceBackup](store, BackupType) {
		if row.ID == BackupRecordID && !row.Archived {
			return &protocol.BackupStatus{At: row.At, Status: row.Status, Entries: row.Entries, Bytes: row.Bytes, Error: row.Error, By: row.By}
		}
	}
	return nil
}

func (m *Monitoring) decideBackup(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *platform.Error) {
	if !c.Automation && !c.Replaying {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	var payload struct {
		Status protocol.BackupStatus `json:"status"`
		Reason string                `json:"reason"`
	}
	if json.Unmarshal(s.GetPayload(), &payload) != nil || (payload.Status.Status != "ok" && payload.Status.Status != "failed") {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	current, _ := platform.Get[protocol.WorkspaceBackup](c, BackupRecordID)
	next := protocol.WorkspaceBackup{
		Record:  current.Record,
		At:      payload.Status.At.UTC(),
		Status:  payload.Status.Status,
		Entries: payload.Status.Entries,
		Bytes:   payload.Status.Bytes,
		Error:   payload.Status.Error,
		Reason:  strings.TrimSpace(payload.Reason),
		By:      cmp.Or(payload.Status.By, c.ID),
	}
	next.ID = BackupRecordID
	next.Archived = false
	if err := c.Check(next); err != nil {
		return nil, err
	}
	return func(r *pb.ChangeRecord) { c.Put(r, next) }, nil
}

func PrepareHostBackup(tenant, subject string, status protocol.BackupStatus, reason string, now time.Time) (platform.Member, *pb.Submission, protocol.AuditEntry) {
	actor := "host:" + subject
	status.By = actor
	if status.At.IsZero() {
		status.At = now.UTC()
	}
	caller := platform.Member{ID: "app:" + ID, Tenant: tenant}
	digest, _ := platform.CanonicalDigest([]any{tenant, actor, status.Status, status.Entries, status.Bytes, status.Error, reason, status.At.UnixNano()})
	if len(digest) > 24 {
		digest = digest[:24]
	}
	sub := &pb.Submission{
		TenantId:       tenant,
		PrincipalId:    caller.ID,
		Authority:      ID,
		IdempotencyKey: "host-backup:" + digest,
		Target:         &pb.EntityRef{Type: BackupType, Id: BackupRecordID},
		Schema:         &pb.SchemaRef{Name: SchemaBackupRecord, Version: 1},
		Payload:        platform.Raw(map[string]any{"status": status, "reason": reason}),
	}
	act := "host.backup.run"
	if status.Status == "failed" {
		act = "host.backup.failed"
	}
	target := tenant
	if reason != "" {
		target += ":" + reason
	}
	audit := protocol.AuditEntry{At: status.At, Member: actor, App: "platform", Action: act, Target: target}
	return caller, sub, audit
}
