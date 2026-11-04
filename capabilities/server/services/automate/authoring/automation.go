package authoring

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	automateowner "platformserver/services/automate"
)

const schemaAlertMigrate = ProcessType + ".migrate-alert"
const schemaAlertDraftMigrate = ProcessType + ".migrate-alert-draft"

func (o *Owner) AcceptedPublicationSchemas() []string {
	return []string{schemaAlertMigrate}
}

func processSubject(object string, run *platform.Run) string {
	if id, ok := strings.CutPrefix(run.Subject, object+"/"); ok {
		return id
	}
	return run.Key
}

func (o *Owner) automationOwnerRefusal(c platform.Caller, id string, p Process, now time.Time) *kernel.Error {
	if p.Kind != "automation" && p.Scheduler == "" {
		return nil
	}
	member, ok := o.env.Host().Member(p.Scheduler)
	if !ok {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The automation owner is no longer available")
	}
	owner := o.env.Host().Caller(c, member, o.env.AppID())
	decision := owner.Authorize(platform.AuthzRequest{Principal: owner.ID, Tenant: owner.Tenant, Kind: "action", Type: ProcessType, Record: id, Action: SchemaProcessRun}, now)
	if !decision.Allow {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The automation owner is no longer authorized")
	}
	return nil
}

func (o *Owner) pauseAutomation(c platform.Caller, id, reason string, now time.Time) {
	if c.Replaying {
		return
	}
	c = o.env.Host().Automation(c, o.env.AppID())
	current, ok := platform.Get[Process](c, id)
	if !ok || current.Paused {
		return
	}
	_, refusal := platform.Decide(c, o.env.Application(), &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: o.env.AppID(),
		Schema: &pb.SchemaRef{Name: SchemaProcessPause, Version: 1}, Target: &pb.EntityRef{Type: ProcessType, Id: id},
		IdempotencyKey: fmt.Sprintf("automation-pause:%s:%d", id, current.Revision), Payload: platform.Raw(map[string]string{"reason": reason})}, now)
	if refusal != nil {
		c.FailDecision(refusal)
	}
}

func (o *Owner) Run(c platform.Caller, now time.Time) *kernel.Error {
	if c.Replaying {
		return nil
	}
	processes, err := o.Inventory()
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	for _, live := range processes {
		p, ok := readPublished[Process](live.Published)
		if !ok || live.Archived || live.Paused || p.Scheduler == "" {
			continue
		}
		if denied := o.automationOwnerRefusal(c, live.ID, p, now); denied != nil {
			o.pauseAutomation(c, live.ID, denied.Message, now)
		}
	}
	alerts, err := o.alertInventory()
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	for _, alert := range alerts {
		if alert.Archived || alert.MigratedVersion == alert.Version && alert.Migrated != "" {
			continue
		}
		schema := schemaAlertMigrate
		if alert.Published == "" && !legacyAlert(alert) {
			schema = schemaAlertDraftMigrate
		}
		_, refusal := platform.Decide(c, o.env.Application(), &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: o.env.AppID(),
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Target: &pb.EntityRef{Type: ProcessType, Id: "alert-" + alert.ID},
			IdempotencyKey: fmt.Sprintf("alert-migrate:%s:%d", alert.ID, alert.Revision), Payload: platform.Raw(map[string]string{"alert": alert.ID})}, now)
		if refusal != nil {
			return refusal
		}
	}
	return nil
}

func (o *Owner) Handles(s *pb.Submission) bool {
	typ := s.GetTarget().GetType()
	if typ == ProcessType || typ == AlertType {
		return true
	}
	name := s.GetSchema().GetName()
	return name == schemaAlertMigrate || name == schemaAlertDraftMigrate || strings.HasPrefix(name, ProcessType+".")
}

func (o *Owner) Decide(ledger *platform.Ledger, c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error, bool) {
	if !o.Handles(s) {
		return nil, nil, false
	}
	name := s.GetSchema().GetName()
	if name == schemaAlertMigrate || name == schemaAlertDraftMigrate {
		record, err := o.migrateAlert(ledger, c, s, now)
		return record, err, true
	}
	if s.GetTarget().GetType() == AlertType && !c.Replaying {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Create an automation with a notification effect instead of an alert rule"), true
	}
	if typ, archive := strings.CutSuffix(name, ".archive"); archive && !c.Replaying && (typ == ProcessType || typ == AlertType) {
		if !c.Holds(c.App, Builder) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}, true
		}
		published := false
		if typ == ProcessType {
			record, _ := platform.Get[Process](c, s.GetTarget().GetId())
			published = record.Published != ""
		} else {
			record, _ := platform.Get[Alert](c, s.GetTarget().GetId())
			published = record.Published != "" || legacyAlert(record)
		}
		if published {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published definition cannot be archived; its installed version must be retained"), true
		}
	}
	record, err, ok := ledger.Generated(c, s, now, nil, o.Entities()...)
	return record, err, ok || o.Handles(s)
}

func (o *Owner) migrateAlert(ledger *platform.Ledger, c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var input struct {
			Alert string `json:"alert"`
		}
		_ = json.Unmarshal(s.GetPayload(), &input)
		a, ok := platform.Get[Alert](c, input.Alert)
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The alert is unavailable")
		}
		current := a
		if err := o.freezeLegacyAlert(&a); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		if a.Published != "" {
			var asset platform.ReleaseAsset
			if err := json.Unmarshal([]byte(a.Published), &asset); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
			fixed, _, err := o.alertFromAsset(asset)
			if err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
			fixed.Record = current.Record
			fixed.Version = a.Version
			a = fixed
		}
		info, ok := o.env.Entity(a.Object)
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Publish the alert's object before migrating it")
		}
		field, _ := info.Field(a.Field)
		op := a.Operator
		if op == "not empty" {
			op = "not-empty"
		}
		trigger := platform.Expr{Kind: "boolean", Op: op, Args: []platform.Expr{platform.ExprReference("json", "subject."+a.Field)}}
		if op != "empty" && op != "not-empty" {
			value, err := platform.ConditionValue(platform.RuleFieldKind(field.Type), a.Value)
			if err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
			if stamp, ok := value.(time.Time); ok {
				if platform.RuleFieldKind(field.Type) == "date" {
					value = stamp.UTC().Format(time.DateOnly)
				} else {
					value = stamp.UTC().Format(time.RFC3339Nano)
				}
			}
			trigger.Args = append(trigger.Args, platform.ExprLiteralValue("json", value))
		}
		id := "alert-" + a.ID
		p, exists := platform.Get[Process](c, id)
		if !exists {
			p = Process{Record: platform.Record{ID: id}, Name: "alert" + a.Name, State: "draft"}
		}
		p.LegacyAlert = a.ID
		p.Kind, p.Title, p.Object, p.Trigger, p.Repeat = "automation", a.Title, a.Object, &trigger, a.Every
		p.Scheduler = cmp.Or(current.Changed.By, current.Created.By)
		p.Steps = []ProcessStep{{Name: "notify", Kind: "notify", Notify: &automateowner.NotificationEffect{Title: a.Title, Body: a.Message, Role: cmp.Or(a.Role, Builder)}}}
		p.Paused = !a.Active
		if current.Published != "" || legacyAlert(current) {
			if p.Version >= 64 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A process may retain at most 64 published versions")
			}
			p.Version++
			p.State = "published"
			p.Published = processImage(p)
			p.Versions = append(p.Versions, p.Published)
			if err := o.validate(p); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
		}
		current.Migrated, current.MigratedVersion = id, current.Version
		applications, err := o.env.PrepareAlertReferences(c, current, p)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		return func(receipt *pb.ChangeRecord) {
			c.Put(receipt, p)
			c.Put(receipt, current)
			applications(receipt)
		}, nil
	})
}
