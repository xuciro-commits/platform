package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

type AcceptedStateReader interface {
	AcceptedValueReader
	StateChanges(context.Context, string, string, string, string, int) ([][]byte, error)
}

func StoredJSONEqual(a, b json.RawMessage) bool {
	x, e := platform.CanonicalDigest(a)
	y, f := platform.CanonicalDigest(b)
	return e == nil && f == nil && x == y
}

type ChangeArchive struct {
	ctx         context.Context
	store       AcceptedStateReader
	tenant, app string
}

func (a ChangeArchive) read(tenant, key, id string, through int) []*pb.ChangeRecord {
	if tenant != a.tenant {
		return nil
	}
	rows, err := a.store.StateChanges(a.ctx, tenant, a.app, key, id, through)
	if err != nil {
		panic(fmt.Errorf("change history read: %w", err))
	}
	out := make([]*pb.ChangeRecord, 0, len(rows))
	for _, raw := range rows {
		r := new(pb.ChangeRecord)
		if err := proto.Unmarshal(raw, r); err != nil {
			panic(err)
		}
		out = append(out, r)
	}
	return out
}

func (a ChangeArchive) Records(tenant string, through int) []*pb.ChangeRecord {
	return a.read(tenant, "", "", through)
}

func (a ChangeArchive) Accepted(tenant, key string, through int) *pb.ChangeRecord {
	rows := a.read(tenant, key, "", through)
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

func (a ChangeArchive) Contains(tenant, id string, through int) bool {
	return len(a.read(tenant, "", id, through)) > 0
}

func NewChangeArchive(ctx context.Context, store AcceptedStateReader, tenant, app string) ChangeArchive {
	return ChangeArchive{ctx: ctx, store: store, tenant: tenant, app: app}
}

type SQLCheckpoint struct {
	Format   string                  `json:"format"`
	Position int64                   `json:"position"`
	Records  persistence.StateDigest `json:"records"`
}

const SQLCheckpointFormat = "platform.sql-checkpoint.v1"

func ReadCheckpoint(raw []byte) (*SQLCheckpoint, error) {
	var header struct {
		Runtime *SQLCheckpoint `json:"runtime"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, err
	}
	if header.Runtime != nil {
		var shape struct {
			Records map[string]json.RawMessage `json:"records"`
		}
		if err := json.Unmarshal(raw, &shape); err != nil {
			return nil, err
		}
		if len(shape.Records) > 0 {
			return nil, fmt.Errorf("SQL owner checkpoint contains portable record state")
		}
	}
	return header.Runtime, nil
}

func SaveCheckpoint(ctx context.Context, j persistence.Journals, tenant string, seq int64, key string, state []byte) error {
	runtime, err := ReadCheckpoint(state)
	if err != nil {
		return err
	}
	if runtime != nil {
		snapshots, ok := j.(persistence.StateSnapshotStorage)
		if !ok {
			return fmt.Errorf("tenant %s: SQL checkpoint archive unavailable", tenant)
		}
		return snapshots.SaveStateSnapshot(ctx, tenant, seq, key, state, runtime.Records)
	}
	return j.SaveSnapshot(ctx, tenant, seq, key, state)
}
