package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	pgx "github.com/jackc/pgx/v5"
)

type AcceptedAppend struct {
	Entry     Entry
	Key, Hash string
}

type BatchJournal interface {
	AppendAcceptedBatch(context.Context, string, []AcceptedAppend) ([][]byte, error)
}

func batchIdentities(identify Identify, tenant string, batch []AcceptedAppend) ([]Identity, error) {
	ids := make([]Identity, len(batch))
	seen := make(map[[3]string]bool, len(batch))
	for i, item := range batch {
		id, err := identify(item.Entry.Body)
		if err != nil || item.Entry.Kind != "accepted-result" || id.Tenant != tenant || id.App != item.Entry.App || id.Key != item.Key || id.Key == "" {
			return nil, fmt.Errorf("journal: invalid batch result identity at %d", i)
		}
		if id.Hash != item.Hash {
			return nil, ErrAcceptedConflict
		}
		index := [3]string{id.App, id.Scope, id.Key}
		if seen[index] {
			return nil, fmt.Errorf("journal: duplicate identity within batch")
		}
		seen[index], ids[i] = true, id
	}
	return ids, nil
}

func (j *Postgres) AppendAcceptedBatch(ctx context.Context, tenant string, batch []AcceptedAppend) ([][]byte, error) {
	return j.appendAcceptedBatch(ctx, tenant, batch, func(ctx context.Context, tx pgx.Tx) error { return tx.Commit(ctx) })
}

func (j *Postgres) batchTenantLock(tenant string) *sync.Mutex {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.locks[tenant] == nil {
		j.locks[tenant] = &sync.Mutex{}
	}
	return j.locks[tenant]
}

func (j *Postgres) batchPriors(ctx context.Context, tx pgx.Tx, tenant string, ids []Identity) ([][]byte, error) {
	answers := make([][]byte, len(ids))
	var query pgx.Batch
	for _, id := range ids {
		query.Queue(`select body,hash from runtime_results where tenant=$1 and app=$2 and scope=$3 and key=$4`, tenant, id.App, id.Scope, id.Key)
	}
	missing := make([]int, 0, len(ids))
	result := tx.SendBatch(ctx, &query)
	for i, id := range ids {
		var hash string
		err := result.QueryRow().Scan(&answers[i], &hash)
		if errors.Is(err, pgx.ErrNoRows) {
			missing = append(missing, i)
			continue
		}
		if err != nil {
			_ = result.Close()
			return nil, err
		}
		if hash != id.Hash {
			_ = result.Close()
			return nil, ErrAcceptedConflict
		}
	}
	if err := result.Close(); err != nil {
		return nil, err
	}
	if len(missing) == 0 {
		return answers, nil
	}
	query = pgx.Batch{}
	for _, i := range missing {
		id := ids[i]
		query.Queue(`select body from journal where tenant=$1 and app=$2 and kind='accepted-result'
    and coalesce(body ->> 'key', body #>> '{submission,idempotencyKey}', body #>> '{receipt,submission,idempotencyKey}')=$3
    and `+acceptedScopeSQL+`=$4`, tenant, id.App, id.Key, id.Scope)
	}
	result = tx.SendBatch(ctx, &query)
	for _, i := range missing {
		err := result.QueryRow().Scan(&answers[i])
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			_ = result.Close()
			return nil, err
		}
		saved, err := j.identify(answers[i])
		if err != nil {
			_ = result.Close()
			return nil, err
		}
		if saved.Hash != ids[i].Hash {
			_ = result.Close()
			return nil, ErrAcceptedConflict
		}
	}
	return answers, result.Close()
}

func (j *Postgres) batchPosition(tenant string, position int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.next[tenant] = max(j.next[tenant], position+1)
}

func (j *Postgres) appendAcceptedBatch(ctx context.Context, tenant string, batch []AcceptedAppend, commit func(context.Context, pgx.Tx) error) ([][]byte, error) {
	ids, err := batchIdentities(j.identify, tenant, batch)
	if err != nil || len(batch) == 0 {
		return nil, err
	}
	lock := j.batchTenantLock(tenant)
	lock.Lock()
	defer lock.Unlock()
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1),63)`, tenant); err != nil {
		return nil, err
	}
	var position int64
	if err = tx.QueryRow(ctx, `select coalesce(max(seq),0) from journal where tenant=$1`, tenant).Scan(&position); err != nil {
		return nil, err
	}
	var statePosition int64
	stateErr := tx.QueryRow(ctx, `select seq from runtime_tenants where tenant=$1 for update`, tenant).Scan(&statePosition)
	active := stateErr == nil
	if !active && !errors.Is(stateErr, pgx.ErrNoRows) {
		return nil, stateErr
	}
	if active && statePosition != position {
		return nil, fmt.Errorf("tenant state position differs")
	}
	j.mu.Lock()
	next, ready := j.next[tenant]
	j.mu.Unlock()
	answers, err := j.batchPriors(ctx, tx, tenant, ids)
	if err != nil {
		return nil, err
	}
	fresh := make([]bool, len(batch))
	for i := range answers {
		fresh[i] = answers[i] == nil
	}
	positions := make([]int64, len(batch))
	var journalBatch pgx.Batch
	for i, item := range batch {
		if !fresh[i] {
			continue
		}
		if !ready || next != position+1 {
			return nil, fmt.Errorf("journal: tenant position changed before batch append")
		}
		e := item.Entry
		position++
		next++
		positions[i] = position
		journalBatch.Queue(`insert into journal (tenant,seq,app,kind,principal,body,at,versions,epoch) values ($1,$2,$3,$4,$5,$6,$7,$8,$9) returning body`,
			tenant, position, e.App, e.Kind, e.Principal, e.Body, e.At, e.Versions, e.Epoch)
	}
	if journalBatch.Len() > 0 {
		results := tx.SendBatch(ctx, &journalBatch)
		for i := range batch {
			if !fresh[i] {
				continue
			}
			if err := results.QueryRow().Scan(&answers[i]); err != nil {
				_ = results.Close()
				return nil, err
			}
		}
		if err := results.Close(); err != nil {
			return nil, err
		}
	}
	if active {
		var resultBatch pgx.Batch
		for i, id := range ids {
			if !fresh[i] {
				continue
			}
			if j.Delta == nil {
				return nil, fmt.Errorf("tenant has no state delta owner")
			}
			delta, err := j.Delta(answers[i])
			if err != nil {
				return nil, err
			}
			if err := j.writeState(ctx, tx, tenant, positions[i], delta); err != nil {
				return nil, err
			}
			resultBatch.Queue(`insert into runtime_results (tenant,app,scope,key,hash,seq,body) values ($1,$2,$3,$4,$5,$6,$7)`, tenant, id.App, id.Scope, id.Key, id.Hash, positions[i], answers[i])
		}
		if resultBatch.Len() > 0 {
			if err := tx.SendBatch(ctx, &resultBatch).Close(); err != nil {
				return nil, err
			}
		}
	}

	if active {
		if _, err := tx.Exec(ctx, `update runtime_tenants set seq=$2 where tenant=$1`, tenant, position); err != nil {
			return nil, err
		}
	}
	if err = commit(ctx, tx); err != nil {
		_ = tx.Rollback(context.Background())
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			return nil, err
		}
		return j.resolveAcceptedBatch(ctx, tenant, ids, fresh, err)
	}
	j.batchPosition(tenant, position)
	return answers, nil
}

func (j *Postgres) resolveAcceptedBatch(ctx context.Context, tenant string, ids []Identity, fresh []bool, commitErr error) ([][]byte, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	unknown := func(err error) ([][]byte, error) { return nil, fmt.Errorf("%w: %v", ErrCommitUnknown, err) }
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return unknown(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1),63)`, tenant); err != nil {
		return unknown(err)
	}
	answers, err := j.batchPriors(ctx, tx, tenant, ids)
	if err != nil {
		return unknown(err)
	}
	newCount, savedCount := 0, 0
	for i := range ids {
		if fresh[i] {
			newCount++
			if answers[i] != nil {
				savedCount++
			}
		} else if answers[i] == nil {
			return unknown(fmt.Errorf("previous result disappeared"))
		}
	}
	if newCount > 0 && savedCount == 0 {
		return nil, fmt.Errorf("batch rolled back: %w", commitErr)
	}
	if savedCount != newCount {
		return unknown(fmt.Errorf("incomplete committed batch"))
	}
	var position int64
	if err = tx.QueryRow(ctx, `select coalesce(max(seq),0) from journal where tenant=$1`, tenant).Scan(&position); err != nil {
		return unknown(err)
	}
	j.batchPosition(tenant, position)
	return answers, nil
}
