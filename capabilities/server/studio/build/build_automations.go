package build

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

func processSubject(object string, run *platform.Run) string {
	if id, ok := strings.CutPrefix(run.Subject, object+"/"); ok {
		return id
	}
	return run.Key
}

func (b *Build) automationOwnerRefusal(c platform.Caller, id string, p Process, now time.Time) *kernel.Error {
	if p.Kind != "automation" && p.Scheduler == "" {
		return nil
	}
	member, ok := b.host.Member(p.Scheduler)
	if !ok {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The automation owner is no longer available")
	}
	owner := b.host.Caller(c, member, ID)
	decision := owner.Authorize(platform.AuthzRequest{Principal: owner.ID, Tenant: owner.Tenant, Kind: "action", Type: ProcessType, Record: id, Action: SchemaProcessRun}, now)
	if !decision.Allow {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The automation owner is no longer authorized")
	}
	return nil
}

func (b *Build) pauseAutomation(c platform.Caller, id, reason string, now time.Time) {
	if c.Replaying {
		return
	}
	c = b.host.Automation(c, ID)
	current, ok := platform.Get[Process](c, id)
	if !ok || current.Paused {
		return
	}
	_, refusal := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID,
		Schema: &pb.SchemaRef{Name: SchemaProcessPause, Version: 1}, Target: &pb.EntityRef{Type: ProcessType, Id: id},
		IdempotencyKey: fmt.Sprintf("automation-pause:%s:%d", id, current.Revision), Payload: platform.Raw(map[string]string{"reason": reason})}, now)
	if refusal != nil {
		c.FailDecision(refusal)
	}
}

func (b *Build) runAutomations(c platform.Caller, now time.Time) *kernel.Error {
	if c.Replaying {
		return nil
	}
	processes, refusal := readDefinitionInventory[Process](c)
	if refusal != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, refusal.Error())
	}
	for _, live := range processes {
		p, ok := ReadPublication[Process](live.Published)
		if !ok || live.Archived || live.Paused || p.Scheduler == "" {
			continue
		}
		if denied := b.automationOwnerRefusal(c, live.ID, p, now); denied != nil {
			b.pauseAutomation(c, live.ID, denied.Message, now)
		}
	}
	alerts, err := readDefinitionInventory[Alert](c)
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
		_, refusal := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID,
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Target: &pb.EntityRef{Type: ProcessType, Id: "alert-" + alert.ID},
			IdempotencyKey: fmt.Sprintf("alert-migrate:%s:%d", alert.ID, alert.Revision), Payload: platform.Raw(map[string]string{"alert": alert.ID})}, now)
		if refusal != nil {
			return refusal
		}
	}
	return nil
}

func (b *Build) migrateAlert(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var input struct {
			Alert string `json:"alert"`
		}
		_ = json.Unmarshal(s.GetPayload(), &input)
		a, ok := platform.Get[Alert](c, input.Alert)
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The alert is unavailable")
		}
		current := a
		if err := freezeLegacyAlert(&a); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		if a.Published != "" {
			var asset platform.ReleaseAsset
			if err := json.Unmarshal([]byte(a.Published), &asset); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
			fixed, _, err := alertFromAsset(asset)
			if err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
			fixed.Record = current.Record
			fixed.Version = a.Version
			a = fixed
		}
		info, ok := b.lookupEntity(a.Object)
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
			p.Published = published(p)
			p.Versions = append(p.Versions, p.Published)
			if err := b.host.BuildProcesses().Validate(b, b.flowOf(p)); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
		}
		current.Migrated, current.MigratedVersion = id, current.Version
		applications, readErr := readDefinitionInventory[Application](c)
		if readErr != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, readErr.Error())
		}
		return func(receipt *pb.ChangeRecord) {
			c.Put(receipt, p)
			c.Put(receipt, current)
			for _, app := range applications {
				changed := false
				for i, ref := range app.Resources {
					if ref.App == ID && ref.Kind == platform.AssetAlert && ref.Name == a.Name {
						app.Resources[i] = platform.AssetRef{App: ID, Kind: platform.AssetFlow, Name: TypeOf(p.Name)}
						changed = true
					}
				}
				if changed {
					c.Put(receipt, app)
				}
			}
		}, nil
	})
}
