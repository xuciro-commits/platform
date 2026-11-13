package records

import (
	"encoding/json"
	"time"

	"platformserver/platform"
)

func CandidateRetainedBodies(candidates map[string]json.RawMessage) []RetainedBody {
	var bodies []RetainedBody
	for _, raw := range candidates {
		var release struct {
			Assets []platform.ReleaseAsset `json:"assets"`
		}
		if json.Unmarshal(raw, &release) != nil {
			continue
		}
		for _, asset := range release.Assets {
			if asset.Ref.Kind == platform.AssetObject {
				bodies = append(bodies, RetainedBody{Type: asset.Ref.Name, Raw: asset.Body})
			}
		}
	}
	return bodies
}

type EnterpriseRecordHit struct {
	Type    *Type
	Row     *Row
	Current bool
}

func EnterpriseRecordHits(store *Store, historicalPaths map[string][][]string, id string, now time.Time, engines ...platform.RuleEngine) []EnterpriseRecordHit {
	var types []*Type
	rows := map[string][]*Row{}
	func() {
		store.Lock()
		defer store.Unlock()
		types = make([]*Type, 0, store.TypeCount())
		for _, et := range store.Types() {
			if len(historicalPaths[et.Info().Type]) == 0 {
				continue
			}
			types = append(types, et)
			for _, r := range et.Rows().All() {
				rows[et.Info().Type] = append(rows[et.Info().Type], r)
			}
		}
	}()
	var hits []EnterpriseRecordHit
	for _, et := range types {
		for _, r := range rows[et.Info().Type] {
			paths := MatchingRefsWithAccess(et.Info().Fields, "", r.Value, id, nil, true)
			historical := HistoryNamesEnterprise(historicalPaths[et.Info().Type], r.RecordHistory(), id)
			if len(paths) == 0 && !historical {
				continue
			}
			hits = append(hits, EnterpriseRecordHit{Type: et, Row: r, Current: len(paths) > 0 && EnterpriseCurrent(et.Info(), r.Value, now, engines...)})
		}
	}
	return hits
}
