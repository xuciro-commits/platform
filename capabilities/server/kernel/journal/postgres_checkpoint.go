package journal

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"strconv"
)

func (v *postgresStateRead) Checkpoint(ctx context.Context, tenant string) (StateDigest, error) {
	return readState(v, ctx, tenant, func(db stateQueries) (StateDigest, error) {
		return stateCheckpoint(ctx, db, tenant, nil)
	})
}

func stateCheckpoint(ctx context.Context, db stateQueries, tenant string, snapshot *int64) (StateDigest, error) {
	suffix, args := "", []any{tenant}
	tables := [3]string{"runtime_records", "runtime_unique", "runtime_values"}
	if snapshot != nil {
		tables = [3]string{"runtime_snapshot_records", "runtime_snapshot_unique", "runtime_snapshot_values"}
		suffix = " and snapshot_seq=$2"
		args = append(args, *snapshot)
	}
	h := sha256.New()
	checkpointPart(h, []byte("platform.sql-checkpoint.v1"))
	out := StateDigest{}
	rows, err := db.Query(ctx, `select type,id,image,query::text,history_count,history_head from `+tables[0]+` where tenant=$1`+suffix+` order by type,id`, args...)
	if err != nil {
		return StateDigest{}, err
	}
	for rows.Next() {
		var typ, id, query, head string
		var image []byte
		var count int
		if err = rows.Scan(&typ, &id, &image, &query, &count, &head); err != nil {
			rows.Close()
			return StateDigest{}, err
		}
		if count < 0 || count > 0 && head == "" {
			rows.Close()
			return StateDigest{}, fmt.Errorf("SQL record history header differs: %s/%s", typ, id)
		}
		for _, part := range [][]byte{[]byte("record"), []byte(typ), []byte(id), image, []byte(query), []byte(strconv.Itoa(count)), []byte(head)} {
			checkpointPart(h, part)
		}
		out.Records++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return StateDigest{}, err
	}
	rows, err = db.Query(ctx, `select type,id,rule,key from `+tables[1]+` where tenant=$1`+suffix+` order by type,id,rule,key`, args...)
	if err != nil {
		return StateDigest{}, err
	}
	for rows.Next() {
		var typ, id, rule, key string
		if err = rows.Scan(&typ, &id, &rule, &key); err != nil {
			rows.Close()
			return StateDigest{}, err
		}
		for _, part := range []string{"unique", typ, id, rule, key} {
			checkpointPart(h, []byte(part))
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return StateDigest{}, err
	}
	rows, err = db.Query(ctx, `select owner,key,image,query::text from `+tables[2]+` where tenant=$1`+suffix+` order by owner,key`, args...)
	if err != nil {
		return StateDigest{}, err
	}
	for rows.Next() {
		var owner, key, query string
		var image []byte
		if err = rows.Scan(&owner, &key, &image, &query); err != nil {
			rows.Close()
			return StateDigest{}, err
		}
		for _, part := range [][]byte{[]byte("value"), []byte(owner), []byte(key), image, []byte(query)} {
			checkpointPart(h, part)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return StateDigest{}, err
	}
	changeQuery := `select app,num,id,key,image from runtime_changes where tenant=$1 order by app,num`
	if snapshot != nil {
		changeQuery = `select c.app,c.num,c.id,c.key,c.image from runtime_changes c join runtime_snapshot_changes p on p.tenant=c.tenant and p.app=c.app and c.num<=p.num where p.tenant=$1 and p.snapshot_seq=$2 order by c.app,c.num`
	}
	rows, err = db.Query(ctx, changeQuery, args...)
	if err != nil {
		return StateDigest{}, err
	}
	previous, ordinal := "", 0
	for rows.Next() {
		var app, id, key string
		var num int
		var image []byte
		if err = rows.Scan(&app, &num, &id, &key, &image); err != nil {
			rows.Close()
			return StateDigest{}, err
		}
		if app != previous {
			previous, ordinal = app, 0
		}
		ordinal++
		if num != ordinal {
			rows.Close()
			return StateDigest{}, fmt.Errorf("SQL receipt prefix differs: %s/%d", app, num)
		}
		for _, part := range [][]byte{[]byte("change"), []byte(app), []byte(strconv.Itoa(num)), []byte(id), []byte(key), image} {
			checkpointPart(h, part)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return StateDigest{}, err
	}
	out.Digest = hex.EncodeToString(h.Sum(nil))
	return out, nil
}

func checkpointPart(h hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	h.Write(size[:])
	h.Write(value)
}
