package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	persistence "platformserver/kernel/store"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/host"
	"platformserver/host/composition"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai"
	agentevaluation "platformserver/services/ai/evaluation"
	"platformserver/services/devops/packages"
	"platformserver/services/objects"
	authoring "platformserver/services/resources/authoring"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	"platformserver/services/work"
	"sync"
)

const (
	tryFile   = "try.yaml"
	tryMember = "author"
	tryRounds = 1000
)

type tryScript struct {
	Roles   map[string]string    `yaml:"roles"`
	At      string               `yaml:"at"`
	Org     tryOrg               `yaml:"org"`
	Members map[string]tryPerson `yaml:"members"`
	Steps   []tryStep            `yaml:"steps"`
}

type tryOrg struct {
	Units     []platform.Unit     `yaml:"units"`
	Parents   map[string]string   `yaml:"parents"`
	Calendars []platform.Calendar `yaml:"calendars"`
}

type tryPerson struct {
	Roles map[string]string `yaml:"roles"`
	Units map[string]string `yaml:"units"`
}

type tryPeriod struct {
	Object     string           `yaml:"object"`
	Groups     []string         `yaml:"groups"`
	Measures   []string         `yaml:"measures"`
	From       string           `yaml:"from"`
	To         string           `yaml:"to"`
	ExpectRows []map[string]any `yaml:"expectRows"`
}

type tryRace struct {
	Submissions    []tryStep `yaml:"submissions"`
	ExpectAccepted int       `yaml:"expectAccepted"`
	ExpectRefused  int       `yaml:"expectRefused"`
	ExpectReason   string    `yaml:"expectReason"`
}

type tryStep struct {
	Do      string         `yaml:"do"`
	Approve string         `yaml:"approve"`
	Reject  string         `yaml:"reject"`
	Period  *tryPeriod     `yaml:"period"`
	Race    *tryRace       `yaml:"race"`
	As      string         `yaml:"as"`
	On      string         `yaml:"on"`
	Read    string         `yaml:"read"`
	With    map[string]any `yaml:"with"`
	Show    []string       `yaml:"show"`
	Expect  map[string]any `yaml:"expect"`
	Refused bool           `yaml:"refused"`
	Reason  string         `yaml:"reason"`
}

type trial struct {
	tenant    *host.Tenant
	member    platform.Member
	members   map[string]platform.Member
	approvals map[string]string
	at        time.Time
	keys      int
	out       io.Writer
}

var placeholder = regexp.MustCompile(`\{([A-Za-z0-9_.-]+)/([^{}./]+)\.([^{}]+)\}`)

func readTryScript(path string) (tryScript, error) {
	var script tryScript
	raw, err := os.ReadFile(path)
	if err != nil {
		return script, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&script); err != nil && !errors.Is(err, io.EOF) {
		return script, fmt.Errorf("%s: %w", path, err)
	}
	for i, step := range script.Steps {
		kinds := 0
		for _, given := range []string{step.Do, step.Read, step.Approve, step.Reject} {
			if given != "" {
				kinds++
			}
		}
		if step.Period != nil {
			kinds++
		}
		if step.Race != nil {
			kinds++
		}
		if kinds != 1 {
			return script, fmt.Errorf("%s: step %d needs exactly one of do, read, approve, reject, period and race", path, i+1)
		}
		if _, known := script.Members[step.As]; step.As != "" && !known {
			return script, fmt.Errorf("%s: step %d acts as %s, who is not under members", path, i+1, step.As)
		}
		if step.Do != "" && step.On == "" {
			return script, fmt.Errorf("%s: step %d (%s) needs on: <type>/<id>", path, i+1, step.Do)
		}
		if step.Read != "" && step.With != nil {
			return script, fmt.Errorf("%s: step %d reads a record and takes no with", path, i+1)
		}
	}
	return script, nil
}

func newTrial(script tryScript, at time.Time, out io.Writer) (*trial, error) {
	roles := script.Roles
	granted := map[string]string{
		controlowner.PlatformApp: controlowner.Admin, authoring.ID: authoring.Builder, ai.ID: ai.Admin,
		work.ID: work.Admin, protocol.AgentApp: protocol.AgentAdmin,
	}
	for app, role := range roles {
		granted[app] = role
	}
	seats := []protocol.Seat{{Subjects: []string{tryMember}, Member: platform.Member{ID: tryMember, Roles: granted}}}
	names := make([]string, 0, len(script.Members))
	for name := range script.Members {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if name == tryMember {
			return nil, fmt.Errorf("members: %s is the trial's own member", name)
		}
		m := script.Members[name]
		seat := protocol.Seat{Subjects: []string{name}, Member: platform.Member{ID: name, Roles: m.Roles}}
		for unit, role := range m.Units {
			seat.Units = append(seat.Units, platform.Membership{Unit: unit, Role: role, Primary: len(m.Units) == 1})
		}
		slices.SortFunc(seat.Units, func(a, b platform.Membership) int { return strings.Compare(a.Unit, b.Unit) })
		seats = append(seats, seat)
	}
	org := platform.OrgSeed{Units: script.Org.Units, Calendars: script.Org.Calendars}
	if len(script.Org.Parents) > 0 {
		org.Structures = []platform.Structure{{ID: "line", Name: "Line", Kind: "line"}}
		for unit, parent := range script.Org.Parents {
			org.Edges = append(org.Edges, platform.Edge{Structure: "line", Unit: unit, Parent: parent})
		}
		slices.SortFunc(org.Edges, func(a, b platform.Edge) int { return strings.Compare(a.Unit, b.Unit) })
	}
	org.Memberships = controlowner.Memberships(seats)
	services := protocol.DefaultRuntimeCapabilities(security.Standard{})
	services.BindOwner, services.NewAgentEvaluator, services.NewTransactionExecutor = protocol.BindOwner, agentevaluation.NewAgentEvaluator, actions.NewRuntime
	tenant, err := host.NewTenantWithCapabilities("try", services, composition.BuiltinApps("try", org, seats...)...)
	if err != nil {
		return nil, err
	}
	tenant.AcceptResult = func(_ context.Context, e host.Entry, _, _ string) (*persistence.AcceptedWrite, error) {
		return &persistence.AcceptedWrite{Body: e.Body}, nil
	}
	member, ok := tenant.Member(tryMember)
	if !ok {
		return nil, errors.New("the trial member is not seated")
	}
	tr := &trial{tenant: tenant, member: member, members: map[string]platform.Member{tryMember: member}, approvals: map[string]string{}, at: at, out: out}
	for _, name := range names {
		m, ok := tenant.Member(name)
		if !ok {
			return nil, fmt.Errorf("members: %s is not seated", name)
		}
		tr.members[name] = m
	}
	return tr, nil
}

func (tr *trial) submit(authority, schema, typ, id string, payload any) error {
	return tr.submitAs(tr.member, authority, schema, typ, id, payload)
}

func (tr *trial) submitAs(who platform.Member, authority, schema, typ, id string, payload any) error {
	tr.keys++
	tr.approvals[typ+"/"+id] = authority + ".try-" + strconv.Itoa(tr.keys)
	sub := &pb.Submission{TenantId: tr.tenant.ID, PrincipalId: who.ID, Authority: authority,
		IdempotencyKey: fmt.Sprintf("try-%d", tr.keys), Target: &pb.EntityRef{Type: typ, Id: id},
		Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
	if _, refusal := tr.tenant.Submit(who, sub, tr.at); refusal != nil {
		lines := []string{refusal.Code.String() + ": " + refusal.Message}
		sub.IdempotencyKey += "-simulated"
		if simulated, err := tr.tenant.Simulate(who, "", "", sub, tr.at); err == nil {
			for _, issue := range simulated.Issues {
				lines = append(lines, string(platform.Raw(issue)))
			}
			for _, step := range simulated.Trace {
				if step.Refusal != "" || step.Authorization != nil && !step.Authorization.Allow {
					where := strings.TrimSpace(step.Kind + " " + step.Operation + " " + step.Type + "/" + step.ID)
					if step.Request != nil {
						where += " (" + step.Request.Kind + " " + step.Request.Action + ")"
					}
					lines = append(lines, "refused at "+where+": "+step.Refusal)
				}
			}
		}
		return errors.New(strings.Join(lines, "\n    "))
	}
	tr.settle()
	return nil
}

func (tr *trial) settle() {
	for range tryRounds {
		if !tr.tenant.Round(tr.at, 1) {
			return
		}
	}
	fmt.Fprintf(tr.out, "  work did not settle after %d rounds\n", tryRounds)
}

func (tr *trial) install(b built) error {
	started := time.Now()
	preview, err := tr.tenant.InspectPackage(context.Background(), protocol.PackageInspectRequest{Content: json.RawMessage(b.artifact)})
	if err != nil {
		return fmt.Errorf("%s@%s: inspect: %w", b.id, b.version, err)
	}
	if err := tr.submit(controlowner.PlatformApp, packages.SchemaPackageImport, protocol.PackageType, b.id, map[string]any{"id": b.id, "digest": preview.Digest}); err != nil {
		return fmt.Errorf("%s@%s: import refused: %w", b.id, b.version, err)
	}
	if err := tr.submit(controlowner.PlatformApp, packages.SchemaPackageInstall, protocol.PackageType, b.id, map[string]any{"id": b.id, "version": b.version}); err != nil {
		return fmt.Errorf("%s@%s: install refused: %w", b.id, b.version, err)
	}
	tr.settle()
	if err := tr.referenceRecords(b); err != nil {
		return fmt.Errorf("%s@%s: %w", b.id, b.version, err)
	}
	fmt.Fprintf(tr.out, "installed %s@%s (%d resources) in %s\n", b.id, b.version, b.assets, time.Since(started).Round(time.Millisecond))
	return nil
}

func (tr *trial) referenceRecords(b built) error {
	var artifact struct {
		Blueprint struct {
			Assets []struct {
				Ref  platform.AssetRef `json:"ref"`
				Body struct {
					Records []struct {
						ID string `json:"id"`
					} `json:"records"`
				} `json:"body"`
			} `json:"assets"`
		} `json:"blueprint"`
	}
	if err := json.Unmarshal([]byte(b.artifact), &artifact); err != nil {
		return err
	}
	var missing []string
	for _, asset := range artifact.Blueprint.Assets {
		if asset.Ref.Kind != platform.AssetObject {
			continue
		}
		for _, row := range asset.Body.Records {
			if _, err := tr.record(asset.Ref.Name + "/" + row.ID); err != nil {
				missing = append(missing, asset.Ref.Name+"/"+row.ID)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("the install did not write these reference records: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (tr *trial) record(ref string) (map[string]any, error) {
	return tr.recordAs(tr.member, ref)
}

func (tr *trial) recordAs(who platform.Member, ref string) (map[string]any, error) {
	typ, id, ok := strings.Cut(ref, "/")
	if !ok || typ == "" || id == "" {
		return nil, fmt.Errorf("%q is not <type>/<id>", ref)
	}
	view, refusal := tr.tenant.RecordOf(context.Background(), who, typ, id, tr.at)
	if refusal != nil {
		return nil, fmt.Errorf("read %s: %s", ref, refusal.Message)
	}
	var rec map[string]any
	if err := json.Unmarshal(platform.Raw(view.Record), &rec); err != nil {
		return nil, fmt.Errorf("read %s: %w", ref, err)
	}
	return rec, nil
}

func (tr *trial) resolve(value any) (any, error) {
	switch v := value.(type) {
	case string:
		if whole := placeholder.FindStringSubmatchIndex(v); whole != nil && whole[0] == 0 && whole[1] == len(v) {
			return tr.lookup(placeholder.FindStringSubmatch(v))
		}
		var failure error
		out := placeholder.ReplaceAllStringFunc(v, func(match string) string {
			found, err := tr.lookup(placeholder.FindStringSubmatch(match))
			if err != nil {
				failure = err
				return match
			}
			if s, ok := found.(string); ok {
				return s
			}
			return string(platform.Raw(found))
		})
		return out, failure
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			resolved, err := tr.resolve(item)
			if err != nil {
				return nil, err
			}
			out[key] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			resolved, err := tr.resolve(item)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	}
	return value, nil
}

func (tr *trial) lookup(match []string) (any, error) {
	rec, err := tr.record(match[1] + "/" + match[2])
	if err != nil {
		return nil, err
	}
	value, ok := field(rec, match[3])
	if !ok || value == nil {
		return nil, fmt.Errorf("%s/%s has no value at %s", match[1], match[2], match[3])
	}
	return value, nil
}

func field(rec map[string]any, path string) (any, bool) {
	var current any = rec
	for _, part := range strings.Split(path, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		if current, ok = m[part]; !ok {
			return nil, false
		}
	}
	return current, true
}

func subset(want, got any) bool {
	if w, ok := want.(map[string]any); ok {
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for key, item := range w {
			if !subset(item, g[key]) {
				return false
			}
		}
		return true
	}
	return sameJSON(want, got)
}

func (tr *trial) run(script tryScript) error {
	for i, step := range script.Steps {
		who := tr.member
		if step.As != "" {
			who = tr.members[step.As]
		}
		if step.Period != nil {
			fmt.Fprintf(tr.out, "%d. period %s (%s..%s)\n", i+1, step.Period.Object, step.Period.From, step.Period.To)
			out, refusal := tr.tenant.AggregateObjectSet(who, objects.ObjectSetRequest{
				Set: platform.ObjectSet{Object: step.Period.Object}, Groups: step.Period.Groups,
				Measures: step.Period.Measures, From: step.Period.From, To: step.Period.To,
			}, tr.at)
			if refusal != nil {
				return fmt.Errorf("step %d: period refused: %s", i+1, refusal.Message)
			}
			var gotRows []map[string]any
			_ = json.Unmarshal(platform.Raw(out.Rows), &gotRows)
			for _, wantRaw := range step.Period.ExpectRows {
				resolved, err := tr.resolve(wantRaw)
				if err != nil {
					return fmt.Errorf("step %d: %w", i+1, err)
				}
				var want map[string]any
				_ = json.Unmarshal(platform.Raw(resolved), &want)
				matched := slices.ContainsFunc(gotRows, func(got map[string]any) bool { return subset(want, got) })
				if !matched {
					return fmt.Errorf("step %d: period rows %s did not contain %s", i+1, platform.Raw(gotRows), platform.Raw(want))
				}
			}
			continue
		}
		if step.Race != nil {
			fmt.Fprintf(tr.out, "%d. race (%d submissions)\n", i+1, len(step.Race.Submissions))
			type prepared struct {
				who platform.Member
				sub *pb.Submission
			}
			items := make([]prepared, len(step.Race.Submissions))
			for idx, subStep := range step.Race.Submissions {
				actor := who
				if subStep.As != "" {
					actor = tr.members[subStep.As]
				}
				targetRaw, err := tr.resolve(subStep.On)
				if err != nil {
					return fmt.Errorf("step %d race[%d]: %w", i+1, idx, err)
				}
				input, err := tr.resolve(map[string]any(subStep.With))
				if err != nil {
					return fmt.Errorf("step %d race[%d]: %w", i+1, idx, err)
				}
				typ, id, _ := strings.Cut(targetRaw.(string), "/")
				tr.keys++
				items[idx] = prepared{who: actor, sub: &pb.Submission{
					TenantId: tr.tenant.ID, PrincipalId: actor.ID, Authority: tr.tenant.AuthorityOf(typ),
					IdempotencyKey: fmt.Sprintf("try-%d", tr.keys), Target: &pb.EntityRef{Type: typ, Id: id},
					Schema: &pb.SchemaRef{Name: subStep.Do, Version: 1}, Payload: platform.Raw(input),
				}}
			}
			var wg sync.WaitGroup
			results := make([]*platform.Error, len(items))
			for idx, item := range items {
				wg.Add(1)
				go func(pos int, p prepared) {
					defer wg.Done()
					_, results[pos] = tr.tenant.Submit(p.who, p.sub, tr.at)
				}(idx, item)
			}
			wg.Wait()
			tr.settle()
			accepted, refused := 0, 0
			var reasons []string
			for _, r := range results {
				if r == nil {
					accepted++
				} else {
					refused++
					reasons = append(reasons, r.Message)
				}
			}
			if accepted != step.Race.ExpectAccepted || refused != step.Race.ExpectRefused {
				return fmt.Errorf("step %d: race got accepted=%d refused=%d (%v), want accepted=%d refused=%d", i+1, accepted, refused, reasons, step.Race.ExpectAccepted, step.Race.ExpectRefused)
			}
			if step.Race.ExpectReason != "" && !slices.ContainsFunc(reasons, func(msg string) bool { return strings.Contains(msg, step.Race.ExpectReason) }) {
				return fmt.Errorf("step %d: race reasons %v did not contain %q", i+1, reasons, step.Race.ExpectReason)
			}
			continue
		}
		ref, err := tr.resolve(step.On + step.Read + step.Approve + step.Reject)
		if err != nil {
			return fmt.Errorf("step %d: %w", i+1, err)
		}
		target := ref.(string)
		if step.Approve != "" || step.Reject != "" {
			verb := map[bool]string{true: "approve", false: "reject"}[step.Approve != ""]
			fmt.Fprintf(tr.out, "%d. %s %s as %s\n", i+1, verb, target, who.ID)
			approval, held := tr.approvals[target]
			if !held {
				return fmt.Errorf("step %d: no action on %s was submitted for approval", i+1, target)
			}
			note, _ := tr.resolve(map[string]any(step.With))
			err := tr.submitAs(who, "work", "work.approval."+verb, "work.approval", approval, note)
			tr.approvals[target] = approval
			if step.Refused {
				if err == nil {
					return fmt.Errorf("step %d: the %s was accepted, the script expects a refusal", i+1, verb)
				}
				fmt.Fprintf(tr.out, "  refused as expected: %v\n", err)
				continue
			}
			if err != nil {
				return fmt.Errorf("step %d: %s refused: %w", i+1, verb, err)
			}
		} else if step.Do != "" {
			fmt.Fprintf(tr.out, "%d. %s %s\n", i+1, step.Do, target)
			input, err := tr.resolve(map[string]any(step.With))
			if err != nil {
				return fmt.Errorf("step %d: %w", i+1, err)
			}
			typ, id, _ := strings.Cut(target, "/")
			authority := tr.tenant.AuthorityOf(typ)
			if authority == "" {
				return fmt.Errorf("step %d: no installed app holds records of type %s", i+1, typ)
			}
			err = tr.submitAs(who, authority, step.Do, typ, id, input)
			if step.Refused {
				if err == nil {
					return fmt.Errorf("step %d: %s was accepted, the script expects a refusal", i+1, step.Do)
				}
				if step.Reason != "" && !strings.Contains(err.Error(), step.Reason) {
					return fmt.Errorf("step %d: %s refusal %q did not contain %q", i+1, step.Do, err.Error(), step.Reason)
				}
				fmt.Fprintf(tr.out, "  refused as expected: %v\n", err)
				continue
			}
			if err != nil {
				return fmt.Errorf("step %d: %s refused: %w", i+1, step.Do, err)
			}
			if (strings.HasPrefix(typ, "platform.") || strings.HasPrefix(typ, "enterprise.")) && len(step.Show) == 0 && len(step.Expect) == 0 {
				continue
			}
		} else {
			fmt.Fprintf(tr.out, "%d. read %s as %s\n", i+1, target, who.ID)
			if step.Refused {
				if _, err := tr.recordAs(who, target); err == nil {
					return fmt.Errorf("step %d: %s could read %s, the script expects a refusal", i+1, who.ID, target)
				}
				fmt.Fprintln(tr.out, "  refused as expected")
				continue
			}
		}
		rec, err := tr.recordAs(who, target)
		if err != nil {
			return fmt.Errorf("step %d: %w", i+1, err)
		}
		show := step.Show
		if len(show) == 0 {
			for key := range rec {
				show = append(show, key)
			}
			slices.Sort(show)
		}
		for _, name := range show {
			value, _ := field(rec, name)
			fmt.Fprintf(tr.out, "  %s: %s\n", name, platform.Raw(value))
		}
		var expect map[string]any
		if err := json.Unmarshal(platform.Raw(step.Expect), &expect); err != nil {
			return fmt.Errorf("step %d: expect: %w", i+1, err)
		}
		names := make([]string, 0, len(expect))
		for name := range expect {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			if value, _ := field(rec, name); !subset(expect[name], value) {
				return fmt.Errorf("step %d: %s %s is %s, the script expects %s", i+1, target, name, platform.Raw(value), platform.Raw(expect[name]))
			}
		}
	}
	return nil
}

func try(dirs []string, scriptPath string, out io.Writer) error {
	if len(dirs) == 0 {
		return errors.New(usage)
	}
	if scriptPath == "" {
		if candidate := filepath.Join(dirs[len(dirs)-1], tryFile); exists(candidate) {
			scriptPath = candidate
		}
	}
	var script tryScript
	at := time.Now().UTC().Truncate(time.Second)
	if scriptPath != "" {
		var err error
		if script, err = readTryScript(scriptPath); err != nil {
			return err
		}
		if script.At != "" {
			if at, err = time.Parse(time.RFC3339, script.At); err != nil {
				return fmt.Errorf("%s: at: %w", scriptPath, err)
			}
		}
	}
	tr, err := newTrial(script, at, out)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		previous, _, err := snapshot(dir)
		if err != nil {
			return err
		}
		b, err := build(dir, previous)
		if err != nil {
			return err
		}
		if err := tr.install(b); err != nil {
			return err
		}
	}
	if scriptPath == "" {
		return nil
	}
	if err := tr.run(script); err != nil {
		return fmt.Errorf("%s: %w", scriptPath, err)
	}
	fmt.Fprintf(out, "%s: %d steps passed\n", filepath.Base(scriptPath), len(script.Steps))
	return nil
}

func installCheck(dirs []string, checked []built, out io.Writer) error {
	order, err := installOrder(checked)
	if err != nil {
		return err
	}
	if len(dirs) != 1 || !exists(filepath.Join(dirs[0], tryFile)) {
		tr, err := newTrial(tryScript{}, time.Now().UTC().Truncate(time.Second), out)
		if err != nil {
			return err
		}
		for _, i := range order {
			if err := tr.install(checked[i]); err != nil {
				return err
			}
		}
	}
	for i, dir := range dirs {
		if !exists(filepath.Join(dir, tryFile)) {
			continue
		}
		needed := requiredBy(checked, i)
		var chain []string
		for _, k := range order {
			if needed[checked[k].id] {
				chain = append(chain, dirs[k])
			}
		}
		if err := try(append(chain, dir), "", out); err != nil {
			return err
		}
	}
	return nil
}

func requiredID(need string) string {
	id, _, _ := strings.Cut(need, "@")
	return strings.TrimSpace(id)
}

func requiredBy(checked []built, i int) map[string]bool {
	byID := map[string]int{}
	for k, b := range checked {
		byID[b.id] = k
	}
	needed := map[string]bool{}
	pending := slices.Clone(checked[i].lock.Requires)
	for len(pending) > 0 {
		id := requiredID(pending[0])
		pending = pending[1:]
		k, known := byID[id]
		if needed[id] || !known {
			continue
		}
		needed[id] = true
		pending = append(pending, checked[k].lock.Requires...)
	}
	return needed
}

func installOrder(checked []built) ([]int, error) {
	byID := map[string]int{}
	for k, b := range checked {
		byID[b.id] = k
	}
	state := make([]int, len(checked))
	var order []int
	var visit func(int) error
	visit = func(k int) error {
		switch state[k] {
		case 1:
			return fmt.Errorf("%s: requires form a cycle", checked[k].id)
		case 2:
			return nil
		}
		state[k] = 1
		for _, need := range checked[k].lock.Requires {
			if dep, known := byID[requiredID(need)]; known {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		state[k] = 2
		order = append(order, k)
		return nil
	}
	for k := range checked {
		if err := visit(k); err != nil {
			return nil, err
		}
	}
	return order, nil
}
