package platformserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Only new object declarations needed by extraction may become visible before
// target activation. They use the original owner's sealed installation image.
func (d *stagedDecision) stageExtractionDefinitions(c platform.Caller, receipt *pb.ChangeRecord, run platform.MigrationRun, prepared *preparedMigration) *kernel.Error {
	if !slices.ContainsFunc(run.Plan.Steps, func(step platform.MigrateStep) bool { return step.Kind == "extract" }) {
		return nil
	}
	definitions := []releaseInstallation{}
	for _, installation := range prepared.installations {
		if installation.Ref == nil || installation.Ref.Kind != platform.AssetObject || d.records.types[installation.Ref.Name] != nil {
			continue
		}
		if installation.Ref.App != c.App {
			return migrationRefusal(fmt.Errorf("extraction declaration crosses its authority"))
		}
		definitions = append(definitions, installation)
	}
	if len(definitions) == 0 {
		return nil
	}
	view, err := d.tenant.installExtractionDefinitions(d.records, definitions)
	if err != nil {
		return migrationRefusal(err)
	}
	if err := d.tenant.anchorReleaseRecords(view.records); err != nil {
		return migrationRefusal(err)
	}
	d.records = view.records
	for _, installation := range definitions {
		typ, id, _ := strings.Cut(installation.Record, "/")
		et := d.records.types[typ]
		v := reflect.New(et.info.Go)
		if err := json.Unmarshal(installation.Image, v.Interface()); err != nil {
			return migrationRefusal(err)
		}
		question := platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "action", Type: typ, Record: id, Action: installation.Schema}
		grant := d.tenant.authorizeFrom(d.records, c, question, d.at, false)
		if err := authorizationRefusal(grant); err != nil {
			return err
		}
		d.RecordAuthorization(c, question, grant)
		if err := d.Put(c, receipt, v.Elem().Interface()); err != nil {
			return err
		}
	}
	d.migrationDefinitions = append(d.migrationDefinitions, definitions...)
	return nil
}

func (t *Tenant) installExtractionDefinitions(recordView *recordStore, definitions []releaseInstallation) (*Tenant, error) {
	view, err := t.assetInstallationView(recordView)
	if err != nil {
		return nil, err
	}
	registry, err := view.assetRegistryLocked()
	if err != nil {
		return nil, err
	}
	for _, installation := range definitions {
		plan, err := view.installationPlan(registry, installation)
		if installation.Ref == nil || installation.Ref.App != "build" || installation.Ref.Kind != platform.AssetObject {
			return nil, fmt.Errorf("invalid extraction object declaration")
		}
		if err != nil {
			return nil, err
		}
		before, err := view.installationPredecessor(plan.Record)
		if err != nil || before != installation.Before || plan.Ref.Kind != platform.AssetObject || view.records.types[plan.Ref.Name] != nil {
			return nil, fmt.Errorf("extraction declaration predecessor differs")
		}
		if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{Private: true}); err != nil {
			return nil, err
		}
	}
	return view, nil
}

// Recovery validates the saved source/target pins and predecessor, then only
// installs saved images. No expression, member policy or migration runs here.
func (t *Tenant) prepareExtractionDefinitions(result acceptedBatch, receipt *pb.ChangeRecord, recordView *recordStore) (*recordStore, error) {
	sub := receipt.GetSubmission()
	if result.App != "build" || sub.GetTarget().GetType() != platform.MigrationRunType || sub.GetSchema().GetName() != platform.MigrationRunType+".start" {
		return nil, fmt.Errorf("object declaration has no migration start")
	}
	var run platform.MigrationRun
	for _, row := range result.Rows {
		if row.Type == platform.MigrationRunType && row.ID == sub.GetTarget().GetId() {
			if err := json.Unmarshal(row.Value, &run); err != nil {
				return nil, err
			}
		}
	}
	if run.ID == "" || run.Plan.From != t.releases.active || !slices.ContainsFunc(run.Plan.Steps, func(step platform.MigrateStep) bool { return step.Kind == "extract" }) {
		return nil, fmt.Errorf("extraction declaration has no saved migration")
	}
	sealed, err := t.releases.candidate(run.Plan.To)
	if err != nil {
		return nil, err
	}
	view, err := t.installExtractionDefinitions(recordView, result.Definitions)
	if err != nil {
		return nil, err
	}
	check, err := view.assetInstallationView(view.records)
	if err != nil {
		return nil, err
	}
	for _, definition := range result.Definitions {
		typ, id, _ := strings.Cut(definition.Record, "/")
		et := check.records.types[typ]
		value := reflect.New(et.info.Go).Elem()
		if err := json.Unmarshal(definition.Image, value.Addr().Interface()); err != nil {
			return nil, err
		}
		installed := *et.rows.Get(id)
		installed.Value, installed.Original = value, nil
		et.setRow(id, &installed)
	}
	assets, err := check.releaseAssetsLocked(nil, false)
	if err != nil {
		return nil, err
	}
	seen := map[platform.AssetRef]bool{}
	for _, definition := range result.Definitions {
		ref := *definition.Ref
		if ref.App != result.App || seen[ref] {
			return nil, fmt.Errorf("extraction declaration repeats or crosses its authority")
		}
		seen[ref] = true
		want := slices.IndexFunc(sealed.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref })
		got := slices.IndexFunc(assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref })
		if want < 0 || got < 0 {
			return nil, fmt.Errorf("extraction declaration is outside its sealed target")
		}
		actual, err := platform.Candidate([]platform.AssetRef{ref}, assets)
		if err != nil {
			return nil, err
		}
		got = slices.IndexFunc(actual.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref })
		same, err := immutableCodeAssetMatches(actual.Assets[got], sealed.Assets[want])
		if err != nil || !same {
			return nil, fmt.Errorf("extraction declaration differs from its sealed target")
		}
	}
	if err := t.anchorReleaseRecords(view.records); err != nil {
		return nil, err
	}
	return view.records, nil
}

func (t *Tenant) applyExtractionDefinitions(definitions []releaseInstallation) error {
	if len(definitions) == 0 {
		return nil
	}
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return err
	}
	for _, installation := range definitions {
		plan, err := t.installationPlan(registry, installation)
		if err != nil {
			return err
		}
		if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{}); err != nil {
			return err
		}
	}
	return nil
}
