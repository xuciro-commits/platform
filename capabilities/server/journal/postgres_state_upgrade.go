package journal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	pgx "github.com/jackc/pgx/v5"

	"slices"
)

func (j *Postgres) StateNeedsUpgrade(ctx context.Context, tenant string) (bool, error) {
	var format int
	err := j.pool.QueryRow(ctx, `select format from runtime_tenants where tenant=$1`, tenant).Scan(&format)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if format < 1 || format > 3 {
		return false, fmt.Errorf("tenant %s has unknown SQL state format %d", tenant, format)
	}
	return format < 3, nil
}

// Earlier SQL writers omitted installation images or model usage from owner
// values. Admit only the exact original accepted preimages, and compare every
// record, history, key and owner value before converting in one transaction.
func (j *Postgres) upgradeStateInstallations(ctx context.Context, tx pgx.Tx, tenant string, position int64, full StateDelta) error {
	var format int
	if err := tx.QueryRow(ctx, `select format from runtime_tenants where tenant=$1`, tenant).Scan(&format); err != nil {
		return err
	}
	if format == 3 {
		return nil
	}
	if format < 1 || format > 3 {
		return fmt.Errorf("unknown SQL state format %d", format)
	}
	legacy := full
	legacy.Records = slices.Clone(full.Records)
	legacy.Values = slices.Clone(full.Values)
	var replacements []StateRecord
	for i, record := range full.Records {
		var actual json.RawMessage
		if err := tx.QueryRow(ctx, `select image from runtime_records where tenant=$1 and type=$2 and id=$3`, tenant, record.Type, record.ID).Scan(&actual); err != nil {
			return err
		}
		if sameJSON(actual, record.Image) {
			continue
		}
		original := full.LegacyImages[record.Type+"/"+record.ID]
		if format != 1 || len(original) == 0 || !sameJSON(actual, original) {
			return fmt.Errorf("tenant %s legacy installation preimage differs: %s/%s", tenant, record.Type, record.ID)
		}
		legacy.Records[i].Image = original
		replacements = append(replacements, record)
	}
	var valueReplacements []StateValue
	for i, value := range full.Values {
		var actual json.RawMessage
		if err := tx.QueryRow(ctx, `select image from runtime_values where tenant=$1 and owner=$2 and key=$3`, tenant, value.Owner, value.Key).Scan(&actual); err != nil {
			return err
		}
		if sameJSON(actual, value.Image) {
			continue
		}
		original := full.LegacyValues[value.Owner+"/"+value.Key]
		if len(original) == 0 || !sameJSON(actual, original) {
			return fmt.Errorf("tenant %s legacy owner state preimage differs: %s/%s", tenant, value.Owner, value.Key)
		}
		legacy.Values[i].Image = original
		valueReplacements = append(valueReplacements, value)
	}
	if err := j.compareState(ctx, tx, tenant, position, legacy); err != nil {
		return err
	}
	for _, record := range replacements {
		if _, err := tx.Exec(ctx, `update runtime_records set image=$4,query=$5::jsonb where tenant=$1 and type=$2 and id=$3`, tenant, record.Type, record.ID, []byte(record.Image), string(record.Image)); err != nil {
			return err
		}
	}
	for _, value := range valueReplacements {
		if _, err := tx.Exec(ctx, `update runtime_values set image=$4,query=$5::jsonb where tenant=$1 and owner=$2 and key=$3`, tenant, value.Owner, value.Key, []byte(value.Image), string(value.Image)); err != nil {
			return err
		}
	}
	// These are disposable SQL read archives, not original accepted bytes.
	// A same-position owner snapshot must not reuse a pre-conversion archive.
	// Portable snapshots and the complete original journal remain untouched.
	if _, err := tx.Exec(ctx, `delete from snapshots s using runtime_snapshot_meta m where s.tenant=$1 and m.tenant=s.tenant and m.snapshot_seq=s.seq`, tenant); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `update runtime_tenants set format=3 where tenant=$1`, tenant)
	return err
}
