package host

import (
	"context"
	"encoding/json"
	"fmt"
	records "platformserver/kernel/records"
	"platformserver/platform"
	trialowner "platformserver/services/devops/trials"
	controlowner "platformserver/services/security/control"
)

func candidateTestTenantWithEnvironment(candidate platform.ReleaseCandidate, member platform.Member, environment simulationEnvironment, others ...platform.Member) (*Tenant, error) {
	apps, b, err := environment.CandidateApps(candidate, member, others...)
	if err != nil {
		return nil, err
	}
	if apps[0].Manifest().ID != controlowner.PlatformApp {
		return nil, fmt.Errorf("candidate identity factory changed its owner")
	}
	sandbox, err := NewTenantWithCapabilities(member.Tenant, environment.Services, apps...)
	if err != nil {
		return nil, err
	}
	sandbox.simulation = true
	endBootstrap, err := b.BeginAssetBootstrap(candidate.Assets)
	if err != nil {
		return nil, err
	}
	defer endBootstrap()
	sandbox.installingSet = true
	if err := sandbox.releases.Put(candidate.ID, candidate.Bytes); err != nil {
		return nil, err
	}
	sandbox.releases.SelectPreview(candidate.ID)
	sandbox.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	err = environment.InstallCandidate(candidate, b, trialowner.CandidateSandbox{
		PutModule: func(digest string, raw []byte) error {
			return sandbox.artifacts().Put(context.Background(), digest, raw)
		},
		RestoreRecords: func(rows map[string][]json.RawMessage) (bool, error) {
			saved := make(map[string][]records.StoredState, len(rows))
			for typ, values := range rows {
				saved[typ] = []records.StoredState{}
				for _, raw := range values {
					saved[typ] = append(saved[typ], records.StoredState{Value: raw})
				}
			}
			held, err := sandbox.restoreRecords(saved)
			return len(held) > 0, err
		},
		InstallPage:        func(page platform.Page) error { return sandbox.InstallPage(b, page) },
		InstallApplication: func(application platform.Application) error { return sandbox.InstallApplication(b, application) },
		AddDefinition:      func(definition platform.Definition) { sandbox.definitions = append(sandbox.definitions, definition) },
		FinishInstall: func() error {
			sandbox.installingSet = false
			return sandbox.validateAssetSet(sandbox.services.Rules)
		},
	})
	if err != nil {
		return nil, err
	}
	return sandbox, nil
}
