package host

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"strings"
	"time"
)

const SchemaVersionHeader = "Platform-Schema-Version"

func (t *Tenant) actionSchemaVersion(app platform.App, action platform.Action) (platform.SchemaVersion, error) {
	return releaseowner.ActionSchemaVersion(t.definitions, app, action)
}

func (t *Tenant) submitSchemaDiagnosed(member platform.Member, sub *pb.Submission, content string, at time.Time) (*pb.ChangeRecord, *kernel.Error, []platform.FieldIssue) {
	var issues []platform.FieldIssue
	record, refusal := t.submit(member, sub, at, submitOptions{report: func(_ *kernel.Error, found []platform.FieldIssue) { issues = found }, versioned: true, contentVersion: strings.TrimSpace(content)})
	return record, refusal, issues
}
