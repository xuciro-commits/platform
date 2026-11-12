package kernel

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

const TenantSnapshotFormat = "platform.tenant.v1"
const TenantSQLSnapshotFormat = "platform.tenant.sql.v1"

type SnapshotCompatibility struct {
	Epoch       int               `json:"epoch"`
	Host        string            `json:"host"`
	Owners      map[string]string `json:"owners"`
	Composition string            `json:"composition"`
}

type SnapshotCheckpoint struct {
	SnapshotCompatibility
	Tenant   string `json:"tenant"`
	Position int64  `json:"position"`
	Build    string `json:"build"`
}

func Compatibility(apps []platform.App) (SnapshotCompatibility, error) {
	compatibility := SnapshotCompatibility{Epoch: persistence.FormatEpoch, Host: TenantSnapshotFormat, Owners: map[string]string{}}
	type compositionApp struct {
		ID, Version string
		Entities    []platform.EntityInfo
		Provides    []platform.Provision
		Consumes    []platform.Consumption
	}
	composition := make([]compositionApp, 0, len(apps))
	for _, app := range apps {
		manifest := app.Manifest()
		if format := strings.TrimSpace(app.SnapshotFormat()); format == "" {
			compatibility.Host = ""
		} else {
			compatibility.Owners[manifest.ID] = format
		}
		item := compositionApp{ID: manifest.ID, Version: manifest.Version, Provides: manifest.Provides, Consumes: manifest.Consumes}
		own := map[reflect.Type]string{}
		for _, entity := range manifest.Entities {
			own[reflect.TypeOf(entity.Model)] = entity.Type
		}
		for _, entity := range manifest.Entities {
			info, err := platform.Describe(manifest.ID, entity, func(goType reflect.Type) string { return own[goType] })
			if err != nil {
				return SnapshotCompatibility{}, err
			}
			item.Entities = append(item.Entities, info)
		}
		slices.SortFunc(item.Entities, func(a, b platform.EntityInfo) int { return strings.Compare(a.Type, b.Type) })
		composition = append(composition, item)
	}
	slices.SortFunc(composition, func(a, b compositionApp) int { return strings.Compare(a.ID, b.ID) })
	digest, err := platform.CanonicalDigest(composition)
	compatibility.Composition = digest
	return compatibility, err
}

func (c SnapshotCompatibility) Key(code string) (string, error) {
	if c.Host == "" {
		return code, nil
	}
	return platform.CanonicalDigest(c)
}

func (c SnapshotCompatibility) RuntimeKey(code string) (string, error) {
	exact := c.Host == ""
	c.Host = TenantSQLSnapshotFormat
	if exact {
		return platform.CanonicalDigest(struct {
			Compatibility SnapshotCompatibility
			Build         string
		}{c, code})
	}
	return platform.CanonicalDigest(c)
}

func (c SnapshotCompatibility) Check(tenant string, raw []byte, position int64, build string) error {
	var header struct {
		Checkpoint *SnapshotCheckpoint `json:"checkpoint"`
		Runtime    *SQLCheckpoint      `json:"runtime"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	if header.Checkpoint == nil && header.Runtime == nil {
		return nil
	}
	saved := header.Checkpoint
	if header.Runtime != nil {
		if saved == nil || header.Runtime.Format != SQLCheckpointFormat || header.Runtime.Position != position {
			return fmt.Errorf("tenant %s: SQL checkpoint format or position differs", tenant)
		}
		if c.Host == "" && saved.Build != build {
			return fmt.Errorf("tenant %s: undeclared owner requires its original checkpoint build", tenant)
		}
		c.Host = TenantSQLSnapshotFormat
	}
	if saved == nil || saved.Tenant != tenant || saved.Position != position || c.Host == "" || !reflect.DeepEqual(saved.SnapshotCompatibility, c) {
		return fmt.Errorf("tenant %s: checkpoint format, composition or position differs", tenant)
	}
	return nil
}
