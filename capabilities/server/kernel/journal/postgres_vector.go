package journal

import (
	"context"
	"encoding/json"
	"fmt"
	pgx "github.com/jackc/pgx/v5"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

var vectorSchema = []string{
	`create extension if not exists vector`,
	`alter table embeddings add column if not exists dimensions integer`,
	`alter table embeddings add column if not exists embedding vector`,
	`create table if not exists embedding_sources (
		tenant text not null, model text not null, source text not null, chunk integer not null,
		hash text not null, scope text not null, apps text[] not null,
		primary key (tenant, model, source, chunk))`,
	`create index if not exists embedding_sources_hash on embedding_sources (tenant, model, hash)`,
	`create index if not exists embedding_sources_apps on embedding_sources using gin (apps)`,
}

func migrateVectors(ctx context.Context, pool *pgxpool.Pool) error {
	for {
		rows, err := pool.Query(ctx, `select tenant, model, hash, vector from embeddings where embedding is null limit 256`)
		if err != nil {
			return err
		}
		type oldVector struct {
			tenant, model, hash string
			value               []float32
		}
		var batch []oldVector
		for rows.Next() {
			var v oldVector
			var raw []byte
			if err := rows.Scan(&v.tenant, &v.model, &v.hash, &raw); err != nil {
				rows.Close()
				return err
			}
			v.value = DecodeVector(raw)
			batch = append(batch, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}
		for _, v := range batch {
			raw, err := json.Marshal(v.value)
			if err != nil || len(v.value) == 0 {
				return fmt.Errorf("invalid stored vector for %s/%s", v.tenant, v.model)
			}
			if _, err := pool.Exec(ctx, `update embeddings set dimensions=$4, embedding=$5::vector where tenant=$1 and model=$2 and hash=$3 and embedding is null`, v.tenant, v.model, v.hash, len(v.value), string(raw)); err != nil {
				return err
			}
		}
	}
	rows, err := pool.Query(ctx, `select distinct dimensions from embeddings where embedding is not null and dimensions<=2000 order by dimensions`)
	if err != nil {
		return err
	}
	var dimensions []int
	for rows.Next() {
		var dimension int
		if err := rows.Scan(&dimension); err != nil {
			rows.Close()
			return err
		}
		dimensions = append(dimensions, dimension)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(dimensions) == 0 {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, dimension := range dimensions {
		if err := vectorIndex(ctx, tx, dimension); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (j *Postgres) SyncVectorSources(ctx context.Context, tenant, model string, sources []VectorSource, reset bool) error {
	if len(sources) == 0 && !reset {
		return nil
	}
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	batch := &pgx.Batch{}
	if reset {
		batch.Queue(`delete from embedding_sources where tenant=$1 and model=$2`, tenant, model)
	}
	for _, source := range sources {
		batch.Queue(`delete from embedding_sources where tenant=$1 and model=$2 and source=$3`, tenant, model, source.Source)
		for _, passage := range source.Passages {
			apps := passage.Apps
			if apps == nil {
				apps = []string{}
			}
			batch.Queue(`insert into embedding_sources (tenant,model,source,chunk,hash,scope,apps) values ($1,$2,$3,$4,$5,$6,$7)`, tenant, model, source.Source, passage.Chunk, passage.Hash, passage.Scope, apps)
		}
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (j *Postgres) MissingVectors(ctx context.Context, tenant, model string, hashes []string) ([]string, error) {
	rows, err := j.pool.Query(ctx, `select h from unnest($3::text[]) h where not exists (select 1 from embeddings e where e.tenant=$1 and e.model=$2 and e.hash=h and e.embedding is not null)`, tenant, model, hashes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var missing []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		missing = append(missing, hash)
	}
	return missing, rows.Err()
}

func vectorIndex(ctx context.Context, tx pgx.Tx, dimensions int) error {
	if dimensions > 2000 {
		return nil
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(1347174740,$1)`, 10000+dimensions); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, fmt.Sprintf(`create index if not exists embeddings_cosine_%d on embeddings using hnsw ((embedding::vector(%d)) vector_cosine_ops) where dimensions=%d`, dimensions, dimensions, dimensions))
	return err
}

func (j *Postgres) NearestVectors(ctx context.Context, tenant, model string, vector []float32, apps []string, limit int) ([]VectorHit, error) {
	if len(vector) == 0 || limit < 1 {
		return nil, nil
	}
	raw, err := json.Marshal(vector)
	if err != nil {
		return nil, err
	}
	if apps == nil {
		apps = []string{}
	}
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `set local hnsw.iterative_scan = strict_order; set local hnsw.ef_search = 100`); err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`select s.source,s.chunk,s.hash,1-(e.embedding::vector(%d) <=> $3::vector(%d)) as score
		from embeddings e join embedding_sources s on (s.tenant=e.tenant and s.model=e.model and s.hash=e.hash)
		where e.tenant=$1 and e.model=$2 and e.dimensions=%d and (cardinality(s.apps)=0 or s.apps && $4::text[])
		order by e.embedding::vector(%d) <=> $3::vector(%d) limit $5`, len(vector), len(vector), len(vector), len(vector), len(vector))
	read := func() ([]VectorHit, error) {
		rows, err := tx.Query(ctx, query, tenant, model, string(raw), apps, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []VectorHit{}
		for rows.Next() {
			var hit VectorHit
			if err := rows.Scan(&hit.Source, &hit.Chunk, &hit.Hash, &hit.Score); err != nil {
				return nil, err
			}
			out = append(out, hit)
		}
		return out, rows.Err()
	}
	hits, err := read()
	if err != nil {
		return nil, err
	}
	if len(hits) < limit {
		if _, err := tx.Exec(ctx, `set local enable_indexscan = off`); err != nil {
			return nil, err
		}
		hits, err = read()
		if err != nil {
			return nil, err
		}
	}
	return hits, tx.Commit(ctx)
}

func (j *Postgres) saveSearchVectors(tenant, model string, vectors map[string][]float32) {
	ctx := context.Background()
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		log.Printf("save vectors: %v", err)
		return
	}
	defer tx.Rollback(ctx)
	dimensions := map[int]bool{}
	for hash, vector := range vectors {
		raw, err := json.Marshal(vector)
		if err != nil || len(vector) == 0 {
			log.Printf("save vectors: invalid vector")
			return
		}
		if _, err := tx.Exec(ctx, `insert into embeddings (tenant,model,hash,vector,dimensions,embedding) values ($1,$2,$3,$4,$5,$6::vector) on conflict (tenant,model,hash) do update set dimensions=excluded.dimensions, embedding=excluded.embedding where embeddings.embedding is null`, tenant, model, hash, EncodeVector(vector), len(vector), string(raw)); err != nil {
			log.Printf("save vectors: %v", err)
			return
		}
		dimensions[len(vector)] = true
	}
	for dimension := range dimensions {
		if err := vectorIndex(ctx, tx, dimension); err != nil {
			log.Printf("vector index: %v", err)
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		log.Printf("save vectors: %v", err)
	}
}
