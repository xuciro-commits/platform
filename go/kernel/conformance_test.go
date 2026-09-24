package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// Runs the shared, language-neutral vectors in Contract/vectors. Schema
// objects inside vectors are parsed strictly with protojson, so vectors must
// match the data contract exactly.

type vectorFile struct {
	Contract string `json:"contract"`
	Vectors  []struct {
		ID        string            `json:"id"`
		Given     json.RawMessage   `json:"given"`
		Steps     []json.RawMessage `json:"steps"`
		ExpectLog map[string]int    `json:"expectLog"`
	} `json:"vectors"`
}

func load(t *testing.T, name string) vectorFile {
	data, err := os.ReadFile("../../vectors/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var f vectorFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func decode(t *testing.T, raw json.RawMessage, m proto.Message) {
	t.Helper()
	if err := protojson.Unmarshal(raw, m); err != nil {
		t.Fatalf("vector does not match schema: %v", err)
	}
}

func refsJSON(refs []Ref) string {
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = r.Type + "/" + r.ID
	}
	return strings.Join(parts, ",")
}

func TestIdentityVectors(t *testing.T) {
	for _, v := range load(t, "k1-identity.json").Vectors {
		t.Run(v.ID, func(t *testing.T) {
			var given struct {
				Entities  []json.RawMessage `json:"entities"`
				Redirects []json.RawMessage `json:"redirects"`
			}
			json.Unmarshal(v.Given, &given)
			var entities []*pb.EntityRef
			for _, raw := range given.Entities {
				e := &pb.EntityRef{}
				decode(t, raw, e)
				entities = append(entities, e)
			}
			id := NewIdentity(entities)
			for _, raw := range given.Redirects {
				r := &pb.Redirect{}
				decode(t, raw, r)
				if err := id.AddRedirect(r); err != nil {
					t.Fatalf("given redirect rejected: %v", err)
				}
			}
			for i, rawStep := range v.Steps {
				var step struct {
					Resolve     json.RawMessage `json:"resolve"`
					AddRedirect json.RawMessage `json:"addRedirect"`
					Expect      struct {
						OK        bool              `json:"ok"`
						Resolved  json.RawMessage   `json:"resolved"`
						Ambiguous []json.RawMessage `json:"ambiguous"`
						Error     string            `json:"error"`
					} `json:"expect"`
				}
				json.Unmarshal(rawStep, &step)
				var got, want string
				switch {
				case step.AddRedirect != nil:
					r := &pb.Redirect{}
					decode(t, step.AddRedirect, r)
					if err := id.AddRedirect(r); err != nil {
						got = err.Error()
					} else {
						got = "ok"
					}
				case step.Resolve != nil:
					ref := &pb.EntityRef{}
					decode(t, step.Resolve, ref)
					refs, err := id.Resolve(ref)
					switch {
					case err != nil:
						got = err.Error()
					case len(refs) == 1:
						got = "resolved:" + refsJSON(refs)
					default:
						got = "ambiguous:" + refsJSON(refs)
					}
				}
				e := step.Expect
				switch {
				case e.Error != "":
					want = e.Error
				case e.OK:
					want = "ok"
				case e.Resolved != nil:
					r := &pb.EntityRef{}
					decode(t, e.Resolved, r)
					want = "resolved:" + refsJSON([]Ref{RefOf(r)})
				default:
					var refs []Ref
					for _, raw := range e.Ambiguous {
						r := &pb.EntityRef{}
						decode(t, raw, r)
						refs = append(refs, RefOf(r))
					}
					want = "ambiguous:" + refsJSON(refs)
				}
				if got != want {
					t.Errorf("step %d: got %s, want %s", i, got, want)
				}
			}
		})
	}
}

func TestChangeRecordVectors(t *testing.T) {
	for _, v := range load(t, "k4-change-record.json").Vectors {
		t.Run(v.ID, func(t *testing.T) {
			var given struct {
				Schemas []json.RawMessage `json:"schemas"`
			}
			json.Unmarshal(v.Given, &given)
			var schemas []*pb.SchemaRef
			for _, raw := range given.Schemas {
				s := &pb.SchemaRef{}
				decode(t, raw, s)
				schemas = append(schemas, s)
			}
			log := NewChangeLog(NewSchemaRegistry(schemas, nil))
			changeIDs := map[int]string{}
			for i, rawStep := range v.Steps {
				var step struct {
					Submit json.RawMessage `json:"submit"`
					At     time.Time       `json:"at"`
					Expect struct {
						Accepted *struct {
							ValidTime    time.Time `json:"validTime"`
							RecordedTime time.Time `json:"recordedTime"`
							SameAs       *int      `json:"sameAs"`
						} `json:"accepted"`
						Error string `json:"error"`
					} `json:"expect"`
				}
				if err := json.Unmarshal(rawStep, &step); err != nil {
					t.Fatal(err)
				}
				s := &pb.Submission{}
				decode(t, step.Submit, s)
				if n, ok := strings.CutPrefix(s.CausationId, "$step:"); ok {
					index, _ := strconv.Atoi(n)
					s.CausationId = changeIDs[index]
				}
				record, err := log.Submit(s, step.At)
				label := fmt.Sprintf("step %d", i)
				if a := step.Expect.Accepted; a != nil {
					if err != nil {
						t.Errorf("%s: rejected with %v, want accepted", label, err)
						continue
					}
					changeIDs[i] = record.GetChangeId()
					if !record.GetValidTime().AsTime().Equal(a.ValidTime) || !record.GetRecordedTime().AsTime().Equal(a.RecordedTime) {
						t.Errorf("%s: times %v/%v, want %v/%v", label, record.GetValidTime().AsTime(),
							record.GetRecordedTime().AsTime(), a.ValidTime, a.RecordedTime)
					}
					if a.SameAs != nil && record.GetChangeId() != changeIDs[*a.SameAs] {
						t.Errorf("%s: replay returned a new change", label)
					}
				} else if err == nil || err.Error() != step.Expect.Error {
					t.Errorf("%s: got %v, want %s", label, err, step.Expect.Error)
				}
			}
			for tenant, count := range v.ExpectLog {
				if got := len(log.Records(tenant)); got != count {
					t.Errorf("log %s: %d records, want %d", tenant, got, count)
				}
			}
		})
	}
}

func TestFactVectors(t *testing.T) {
	for _, v := range load(t, "k2-k3-facts.json").Vectors {
		t.Run(v.ID, func(t *testing.T) {
			var given struct {
				Schemas []json.RawMessage `json:"schemas"`
			}
			json.Unmarshal(v.Given, &given)
			var schemas []*pb.SchemaRef
			for _, raw := range given.Schemas {
				s := &pb.SchemaRef{}
				decode(t, raw, s)
				schemas = append(schemas, s)
			}
			log := NewFactLog(NewSchemaRegistry(schemas, nil))
			factIDs := map[int]string{}
			for i, rawStep := range v.Steps {
				var step struct {
					Record json.RawMessage `json:"record"`
					Claims *struct {
						TenantID  string          `json:"tenantId"`
						Subject   json.RawMessage `json:"subject"`
						Attribute string          `json:"attribute"`
					} `json:"claims"`
					At     time.Time `json:"at"`
					Expect struct {
						Accepted *struct {
							RecordedTime time.Time `json:"recordedTime"`
							SameAs       *int      `json:"sameAs"`
						} `json:"accepted"`
						Current []int  `json:"current"`
						Error   string `json:"error"`
					} `json:"expect"`
				}
				if err := json.Unmarshal(rawStep, &step); err != nil {
					t.Fatal(err)
				}
				label := fmt.Sprintf("step %d", i)
				if q := step.Claims; q != nil {
					subject := &pb.EntityRef{}
					decode(t, q.Subject, subject)
					var got, want []string
					for _, r := range log.CurrentClaims(q.TenantID, subject, q.Attribute) {
						got = append(got, r.GetFactId())
					}
					for _, n := range step.Expect.Current {
						want = append(want, factIDs[n])
					}
					if !slices.Equal(got, want) {
						t.Errorf("%s: current claims %v, want %v", label, got, want)
					}
					continue
				}
				f := &pb.Fact{}
				decode(t, step.Record, f)
				for j, input := range f.DerivedFrom {
					if n, ok := strings.CutPrefix(input, "$step:"); ok {
						index, _ := strconv.Atoi(n)
						f.DerivedFrom[j] = factIDs[index]
					}
				}
				record, err := log.Record(f, step.At)
				if a := step.Expect.Accepted; a != nil {
					if err != nil {
						t.Errorf("%s: rejected with %v, want accepted", label, err)
						continue
					}
					factIDs[i] = record.GetFactId()
					if !record.GetRecordedTime().AsTime().Equal(a.RecordedTime) {
						t.Errorf("%s: recorded %v, want %v", label, record.GetRecordedTime().AsTime(), a.RecordedTime)
					}
					if a.SameAs != nil && record.GetFactId() != factIDs[*a.SameAs] {
						t.Errorf("%s: replay returned a new fact", label)
					}
				} else if err == nil || err.Error() != step.Expect.Error {
					t.Errorf("%s: got %v, want %s", label, err, step.Expect.Error)
				}
			}
			for tenant, count := range v.ExpectLog {
				if got := len(log.Records(tenant)); got != count {
					t.Errorf("log %s: %d records, want %d", tenant, got, count)
				}
			}
		})
	}
}

func TestSchemaEvolutionVectors(t *testing.T) {
	for _, v := range load(t, "k7-schema-evolution.json").Vectors {
		t.Run(v.ID, func(t *testing.T) {
			var given struct {
				Known    []json.RawMessage `json:"known"`
				Upgrades []json.RawMessage `json:"upgrades"`
				Stored   []json.RawMessage `json:"stored"`
			}
			json.Unmarshal(v.Given, &given)
			refs := func(raws []json.RawMessage) (out []*pb.SchemaRef) {
				for _, raw := range raws {
					s := &pb.SchemaRef{}
					decode(t, raw, s)
					out = append(out, s)
				}
				return out
			}
			var upgrades []*pb.UpgradeStep
			for _, raw := range given.Upgrades {
				u := &pb.UpgradeStep{}
				decode(t, raw, u)
				upgrades = append(upgrades, u)
			}
			registry, stored := NewSchemaRegistry(refs(given.Known), upgrades), refs(given.Stored)
			for i, rawStep := range v.Steps {
				var step struct {
					Accepts    json.RawMessage `json:"accepts"`
					AddUpgrade json.RawMessage `json:"addUpgrade"`
					Retire     json.RawMessage `json:"retire"`
					Upgrade    *struct {
						From      json.RawMessage `json:"from"`
						ToVersion uint32          `json:"toVersion"`
					} `json:"upgrade"`
					Negotiate *struct {
						Name     string   `json:"name"`
						Versions []uint32 `json:"versions"`
					} `json:"negotiate"`
					Expect json.RawMessage `json:"expect"`
				}
				if err := json.Unmarshal(rawStep, &step); err != nil {
					t.Fatal(err)
				}
				result := func(value any, err *Error) string {
					if err != nil {
						return fmt.Sprintf(`{"error":%q}`, err.Error())
					}
					out, _ := json.Marshal(value)
					return string(out)
				}
				ok := map[string]bool{"ok": true}
				var got string
				switch {
				case step.Accepts != nil:
					s := &pb.SchemaRef{}
					decode(t, step.Accepts, s)
					var err *Error
					if !registry.Accepts(s) {
						err = errorf(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
					}
					got = result(ok, err)
				case step.AddUpgrade != nil:
					u := &pb.UpgradeStep{}
					decode(t, step.AddUpgrade, u)
					got = result(ok, registry.AddUpgrade(u))
				case step.Retire != nil:
					s := &pb.SchemaRef{}
					decode(t, step.Retire, s)
					got = result(ok, registry.Retire(s, stored))
				case step.Upgrade != nil:
					s := &pb.SchemaRef{}
					decode(t, step.Upgrade.From, s)
					path, err := registry.Path(s, step.Upgrade.ToVersion)
					got = result(map[string][]uint32{"path": path}, err)
				case step.Negotiate != nil:
					version, err := registry.Negotiate(step.Negotiate.Name, step.Negotiate.Versions)
					got = result(map[string]uint32{"version": version}, err)
				}
				var want any
				json.Unmarshal(step.Expect, &want)
				wantJSON, _ := json.Marshal(want)
				if got != string(wantJSON) {
					t.Errorf("step %d: got %s, want %s", i, got, wantJSON)
				}
			}
		})
	}
}

func TestAuthorityVectors(t *testing.T) {
	for _, v := range load(t, "k5-authority.json").Vectors {
		t.Run(v.ID, func(t *testing.T) {
			var given struct {
				Edge         string            `json:"edge"`
				Declarations []json.RawMessage `json:"declarations"`
			}
			json.Unmarshal(v.Given, &given)
			authorities := NewAuthorities(given.Edge)
			for _, raw := range given.Declarations {
				d := &pb.AuthorityDeclaration{}
				decode(t, raw, d)
				if err := authorities.Declare(d); err != nil {
					t.Fatalf("given declaration rejected: %v", err)
				}
			}
			for i, rawStep := range v.Steps {
				var step struct {
					Declare    json.RawMessage `json:"declare"`
					Authorize  json.RawMessage `json:"authorize"`
					Enqueue    json.RawMessage `json:"enqueue"`
					Transition *struct {
						TenantID       string `json:"tenantId"`
						IdempotencyKey string `json:"idempotencyKey"`
						Event          string `json:"event"`
					} `json:"transition"`
					Expect struct {
						OK    bool   `json:"ok"`
						State string `json:"state"`
						Error string `json:"error"`
					} `json:"expect"`
				}
				if err := json.Unmarshal(rawStep, &step); err != nil {
					t.Fatal(err)
				}
				var state pb.SubmissionState
				var err *Error
				switch {
				case step.Declare != nil:
					d := &pb.AuthorityDeclaration{}
					decode(t, step.Declare, d)
					err = authorities.Declare(d)
				case step.Authorize != nil:
					s := &pb.Submission{}
					decode(t, step.Authorize, s)
					err = authorities.Authorize(s)
				case step.Enqueue != nil:
					s := &pb.Submission{}
					decode(t, step.Enqueue, s)
					state, err = authorities.Enqueue(s)
				case step.Transition != nil:
					tr := step.Transition
					state, err = authorities.Transition(tr.TenantID, tr.IdempotencyKey, tr.Event)
				}
				got, want := "ok", "ok"
				if err != nil {
					got = err.Error()
				} else if state != 0 {
					got = state.String()
				}
				if e := step.Expect; e.Error != "" {
					want = e.Error
				} else if e.State != "" {
					want = e.State
				}
				if got != want {
					t.Errorf("step %d: got %s, want %s", i, got, want)
				}
			}
		})
	}
}
