// Command new-app scaffolds an app (ADR-0023 D8) that already runs, so that a
// person — or a coding agent — walks the path of docs/Apps.md by changing it:
// create app → declare entities → declare actions → declare flows → add
// translations → run. Run it in capabilities/server:
//
//	go run ./cmd/new-app -id purchasing -entity request -title "Purchase request" -zh 采购申请 -app-zh 采购
//
// It writes apps/<id>/server (the app, its tests, its dictionary and a
// development host) and, unless -web=false, web/packages/<id> (its UI), which
// it registers in the workspace.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"
)

type app struct {
	ID, Entity, Type, Title, Lower, Plural, Zh, AppTitle, AppZh string
}

func main() {
	var a app
	root := flag.String("root", "../..", "the repository's root")
	web := flag.Bool("web", true, "also write the UI package and register it in the workspace")
	flag.StringVar(&a.ID, "id", "", "the app's ID and Go package, lower case: purchasing")
	flag.StringVar(&a.Entity, "entity", "item", "its first entity type's name, lower case: request")
	flag.StringVar(&a.Title, "title", "", "the entity type's title: Purchase request (default: from -entity)")
	flag.StringVar(&a.Zh, "zh", "", "the title in Simplified Chinese (default: the title; translate it in step 5)")
	flag.StringVar(&a.AppTitle, "app-title", "", "the app's title (default: from -id)")
	flag.StringVar(&a.AppZh, "app-zh", "", "the app's title in Simplified Chinese (default: the app's title)")
	flag.Parse()
	if !regexp.MustCompile(`^[a-z][a-z0-9]*$`).MatchString(a.ID) || !regexp.MustCompile(`^[a-z][a-z0-9]*$`).MatchString(a.Entity) {
		fail("-id and -entity are lower-case letters and digits, starting with a letter")
	}
	a.Title = or(a.Title, strings.ToUpper(a.Entity[:1])+a.Entity[1:])
	a.Type = strings.ToUpper(a.Entity[:1]) + a.Entity[1:]
	a.Lower = strings.ToLower(a.Title)
	a.Plural = plural(a.Title)
	a.Zh = or(a.Zh, a.Title)
	a.AppTitle = or(a.AppTitle, strings.ToUpper(a.ID[:1])+a.ID[1:])
	a.AppZh = or(a.AppZh, a.AppTitle)

	dir := filepath.Join(*root, "apps", a.ID, "server")
	if _, err := os.Stat(dir); err == nil {
		fail(dir + " exists")
	}
	write(filepath.Join(dir, "go.mod"), goMod, a)
	write(filepath.Join(dir, a.ID+".go"), appGo, a)
	write(filepath.Join(dir, a.ID+"_test.go"), testGo, a)
	write(filepath.Join(dir, "cmd", a.ID+"-server", "main.go"), serverGo, a)
	dictionary(filepath.Join(dir, "i18n", "zh-CN.json"), a)
	run(dir, "go", "mod", "tidy")
	run(dir, "go", "fmt", "./...")
	fmt.Printf("wrote %s: go test ./... there, then go run ./cmd/%s-server\n", dir, a.ID)
	if !*web {
		return
	}
	pkg := filepath.Join(*root, "web", "packages", a.ID)
	write(filepath.Join(pkg, "package.json"), packageJSON, a)
	write(filepath.Join(pkg, "tsconfig.json"), tsconfig, a)
	write(filepath.Join(pkg, "src", "index.tsx"), indexTSX, a)
	write(filepath.Join(pkg, "src", "i18n.ts"), i18nTS, a)
	register(filepath.Join(*root, "web", "apps", "workspace"), a)
	run(filepath.Join(*root, "web"), "pnpm", "install", "--offline")
	fmt.Printf("wrote %s and registered it in the workspace: pnpm --dir web/apps/workspace build, then the host with -web\n", pkg)
}

// dictionary is the app's Simplified Chinese: its own words. Generated
// actions ("Create purchase request") are said from them by the platform's
// patterns.
func dictionary(path string, a app) {
	words := map[string]string{
		a.AppTitle: a.AppZh, a.Title: a.Zh, a.Plural: a.Zh,
		"A " + a.Lower + " people work on until a manager marks it done.": "人们处理、直到经理标记为完成的" + a.Zh + "。",
		"What it is about": "它是关于什么的", "Who created it": "创建人", "Finish": "完成", "Mark it done.": "标记为已完成。",
		"Review": "审核", "A manager reviews it": "经理审核", "Finish it": "完成它", "Review {id}": "审核 {id}",
		"finish": "完成", "keep open": "保持进行中",
	}
	raw, _ := json.MarshalIndent(words, "", " ")
	mustWrite(path, append(raw, '\n'))
}

// register adds the UI package to the workspace: its dependency and its entry.
func register(workspace string, a app) {
	pj := filepath.Join(workspace, "package.json")
	raw, err := os.ReadFile(pj)
	check(err)
	var doc map[string]any
	check(json.Unmarshal(raw, &doc))
	deps := doc["dependencies"].(map[string]any)
	deps["@pkg/"+a.ID] = "workspace:*"
	out, _ := json.MarshalIndent(doc, "", "  ")
	mustWrite(pj, append(out, '\n'))
	app := filepath.Join(workspace, "src", "App.tsx")
	src, err := os.ReadFile(app)
	check(err)
	anchor := `  { serves: ["platform",`
	i := bytes.Index(src, []byte(anchor))
	if i < 0 {
		fail("the workspace's list of UI packages is not where it was (" + app + ")")
	}
	entry := fmt.Sprintf("  { serves: [%q], load: () => import(%q) },\n", a.ID, "@pkg/"+a.ID)
	mustWrite(app, slices.Concat(src[:i], []byte(entry), src[i:]))
}

func plural(t string) string {
	switch {
	case strings.HasSuffix(t, "y") && !strings.ContainsAny(t[len(t)-2:len(t)-1], "aeiou"):
		return t[:len(t)-1] + "ies"
	case strings.HasSuffix(t, "s"), strings.HasSuffix(t, "x"), strings.HasSuffix(t, "ch"), strings.HasSuffix(t, "sh"):
		return t + "es"
	}
	return t + "s"
}

func write(path, text string, a app) {
	var b bytes.Buffer
	check(template.Must(template.New(path).Delims("[[", "]]").Parse(text)).Execute(&b, a))
	mustWrite(path, b.Bytes())
}

func mustWrite(path string, b []byte) {
	check(os.MkdirAll(filepath.Dir(path), 0o755))
	check(os.WriteFile(path, b, 0o644))
}

func run(dir string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, os.Stdout, os.Stderr
	check(cmd.Run())
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "new-app:", msg)
	os.Exit(1)
}

const goMod = `module [[.ID]]

go 1.27.1

require (
	platformkernel v0.0.0
	platformserver v0.0.0
)

replace platformkernel => ../../../contract/go

replace platformserver => ../../../capabilities/server
`

const appGo = `// Package [[.ID]] is the [[.AppTitle]] app, scaffolded by capabilities/server/cmd/new-app
// (ADR-0023 D8). docs/Apps.md walks its path; each step is marked below.
package [[.ID]]

import (
	"embed"
	"encoding/json"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// 1. Create app: its ID, its roles, and the data classes it is authority for.
const (
	ID        = "[[.ID]]"
	[[.Type]]Type = "[[.ID]].[[.Entity]]"
	Member    = "member"  // works on their own [[.Lower]]s
	Manager   = "manager" // sees every [[.Lower]], reviews and finishes them
)

// 2. Declare entities: a Go struct is an entity type. Tags say how its fields
// read (field, choices, type) and what they mean (help, synonyms, example).
type [[.Type]] struct {
	platform.Record
	Title string ` + "`" + `json:"title" field:"required,search" help:"What it is about"` + "`" + `
	Owner string ` + "`" + `json:"owner" field:"readonly" help:"Who created it"` + "`" + `
	State string ` + "`" + `json:"state" field:"readonly" choices:"open,done"` + "`" + `
}

func Entities() []platform.Entity {
	return []platform.Entity{{Type: [[.Type]]Type, Title: "[[.Title]]", Model: [[.Type]]{},
		Description: "A [[.Lower]] people work on until a manager marks it done.",
		Scope:       platform.Scope{Owner: "owner", Levels: map[string]string{Member: platform.ScopeOwn}},
		// 3. Declare actions: create, edit and archive are generated, and each
		// transition of the lifecycle is an action with its roles and rules.
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Member, Manager}},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "open",
			States: []platform.State{{Name: "open", Title: "Open", Tone: "info"}, {Name: "done", Title: "Done", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "finish", Title: "Finish", Description: "Mark it done.",
				From: []string{"open"}, To: []string{"done"}, Roles: []string{Manager}}}}}}
}

// 4. Declare flows: a manager reviews each new [[.Lower]] and may finish it.
// Each step's reason is kept on the instance; replay takes it again.
func Review() platform.Flow {
	return platform.Flow{Name: "review", Title: "Review", Version: 1, Owners: []string{Manager},
		Start: platform.Start{On: []string{[[.Type]]Type + ".create"}, Begin: func(c platform.Caller, e platform.Event) (string, any, bool) {
			return e.Record.GetSubmission().GetTarget().GetId(), nil, true
		}},
		Steps: []platform.Step{
			{Name: "review", Title: "A manager reviews it", Ask: &platform.Ask{
				Title:   func(c platform.Caller, r *platform.Run) string { return "Review " + r.Key },
				Ref:     func(c platform.Caller, r *platform.Run) string { return [[.Type]]Type + "/" + r.Key },
				To:      func(platform.Caller, *platform.Run) []platform.Recipient { return []platform.Recipient{{AppRole: Manager}} },
				Answers: []string{"finish", "keep open"}},
				Choose: func(c platform.Caller, r *platform.Run) (string, string) {
					if r.Answer == "finish" {
						return "finish", "the manager finished it"
					}
					return "", "the manager keeps it open"
				}},
			{Name: "finish", Title: "Finish it", Act: &platform.Act{Action: [[.Type]]Type + ".finish",
				Target:  func(c platform.Caller, r *platform.Run) string { return r.Key },
				Payload: func(platform.Caller, *platform.Run) any { return map[string]any{} }}},
		}}
}

// 5. Add translations: i18n/<language>.json, keyed by the English text. The
// platform says generated actions from your words; TestChinese lists what is missing.
//
//go:embed i18n
var languageFiles embed.FS

var languages = platform.LoadLanguages(languageFiles, "i18n")

// App is the app in one tenant; the host keeps its records (ADR-0016).
type App struct {
	mu     sync.Mutex
	ledger *platform.Ledger
}

func New(tenant string) *App {
	return &App{ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(platform.EntityActions(Entities()[0])...), [[.Type]]Type)}
}

func (a *App) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: "[[.AppTitle]]", Version: "1", Actions: a.ledger.Catalog, Entities: Entities(),
		Flows: []platform.Flow{Review()}, Languages: languages}
}

func (a *App) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }
func (a *App) Snapshot() (json.RawMessage, error)       { return a.ledger.Snapshot() }
func (a *App) Restore(raw json.RawMessage) error        { return a.ledger.Restore(raw) }

func (a *App) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}

func (a *App) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// Submit decides the app's actions: here only the generated ones and the
// lifecycle's; an action with rules of its own is decided below them.
func (a *App) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if record, err, ok := a.ledger.Generated(c, s, now, nil, Entities()...); ok {
		return record, err
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}
`

const testGo = `package [[.ID]]

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
)

// A member creates a [[.Lower]], the review flow asks a manager, the manager
// finishes it; replay rebuilds all of it (CheckReplay).
func TestApp(t *testing.T) {
	var journal []platformserver.Entry
	build := func() *platformserver.Tenant {
		seat := func(id, role string) platformserver.Seat {
			return platformserver.Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{ID: role}}}
		}
		tn, err := platformserver.NewTenant("t", platformserver.NewConsole("t", seat("ana", Member), seat("mo", Manager)),
			platformserver.NewWork("t"), platformserver.NewFlows("t"), New("t"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e platformserver.Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.Member(id); return m }
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	keys := 0
	do := func(who, authority, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(member(who), &pb.Submission{TenantId: "t", PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	expect := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: got %s, want %s", what, got, want)
		}
	}
	work := func() {
		for range 3 {
			now = now.Add(time.Second)
			tn.Work(now)
		}
	}
	state := func(id string) string {
		v, err := tn.RecordOf(member("mo"), [[.Type]]Type, id, now)
		if err != nil {
			return err.Error()
		}
		return v.Record.([[.Type]]).State
	}

	expect("create", do("ana", ID, [[.Type]]Type+".create", [[.Type]]Type, "X-1", map[string]string{"title": "First"}), "ok")
	expect("a member cannot finish it", do("ana", ID, [[.Type]]Type+".finish", [[.Type]]Type, "X-1", map[string]any{}), "ERROR_CODE_POLICY_DENIED")
	work()
	out, _ := tn.Read(member("mo"), "inbox")
	tasks := out.([]platformserver.WorkTask)
	i := slices.IndexFunc(tasks, func(x platformserver.WorkTask) bool { return x.Title == "Review X-1" })
	if i < 0 {
		t.Fatalf("no review task in %v", tasks)
	}
	expect("answer", do("mo", platformserver.WorkApp, "work.task.complete", platformserver.TaskType, tasks[i].ID, map[string]string{"answer": "finish"}), "ok")
	work()
	expect("finished by the flow", state("X-1"), "done")
	platformserver.CheckReplay(t, tn, journal, build)
}

// Every text of the app reads in Simplified Chinese (AGENTS.md rule 10).
func TestChinese(t *testing.T) {
	tn, err := platformserver.NewTenant("t", platformserver.NewConsole("t"), platformserver.NewWork("t"), platformserver.NewFlows("t"), New("t"))
	if err != nil {
		t.Fatal(err)
	}
	if missing := tn.Untranslated(ID, "zh-CN"); len(missing) > 0 {
		t.Errorf("add to i18n/zh-CN.json: %q", missing)
	}
}
`

const serverGo = `// Command [[.ID]]-server runs the [[.AppTitle]] app on a development host: step 6
// of docs/Apps.md. With -web the host serves the workspace's build, and the
// development tokens "manager" and "member" sign in.
package main

import (
	"flag"
	"log"

	"[[.ID]]"
	"platformserver"
	"platformserver/platform"
)

func main() {
	deployment := platformserver.Flags("127.0.0.1:8499")
	flag.Parse()
	seat := func(token, id string, roles map[string]string) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{token}, Member: platform.Member{ID: id, Roles: roles}}
	}
	seats := deployment.Seats([]platformserver.Seat{
		seat("manager", "manager-1", map[string]string{[[.ID]].ID: [[.ID]].Manager, platformserver.PlatformApp: platformserver.Admin,
			platformserver.WorkApp: platformserver.WorkAdmin, platformserver.FlowApp: platformserver.FlowAdmin}),
		seat("member", "member-1", map[string]string{[[.ID]].ID: [[.ID]].Member}),
	})
	t, err := platformserver.NewTenant("dev", platformserver.NewConsole("dev", seats...), platformserver.NewWork("dev"), platformserver.NewFlows("dev"), [[.ID]].New("dev"))
	if err == nil {
		err = deployment.Serve(t)
	}
	log.Fatal(err)
}
`

const packageJSON = `{
  "name": "@pkg/[[.ID]]",
  "private": true,
  "type": "module",
  "exports": {
    ".": "./src/index.tsx"
  },
  "scripts": {
    "typecheck": "tsc -p .",
    "test": "true",
    "build": "true"
  },
  "dependencies": {
    "@platform/app": "workspace:*",
    "@platform/kernel": "workspace:*",
    "@platform/ui": "workspace:*",
    "lucide-react": "^1.47.0",
    "react": "^19.3.0"
  },
  "devDependencies": {
    "@types/react": "^19.3.0",
    "typescript": "^7.0.2"
  }
}
`

const tsconfig = `{ "extends": "../../tsconfig.base.json", "include": ["src"] }
`

const indexTSX = `// The [[.AppTitle]] app's UI (scaffolded, ADR-0023 D8): its records' list, pages
// and forms are generated from the declaration; the host decides who sees what.
import "./i18n";
import { GeneratedForm, Records, defineApp, newId, useHost } from "@platform/app";
import { Button, Dialog, t } from "@platform/ui";
import { ListChecks, Plus } from "lucide-react";
import { useState } from "react";

function List() {
  const { can, decide } = useHost();
  const [creating, setCreating] = useState(false);
  return (
    <>
      <Records type="[[.ID]].[[.Entity]]" actions={can("[[.ID]].[[.Entity]].create") && <Button variant="primary" onClick={() => setCreating(true)}><Plus />{t("New")}</Button>} />
      <Dialog open={creating} onOpenChange={setCreating} title={t("New")}>
        <GeneratedForm type="[[.ID]].[[.Entity]]" submitLabel={t("Save")} onCancel={() => setCreating(false)}
          onSubmit={async (v) => { if (await decide("[[.ID]].[[.Entity]].create", { type: "[[.ID]].[[.Entity]]", id: newId("[[.ID]]") }, v, { expectedRevision: 0 })) setCreating(false); }} />
      </Dialog>
    </>
  );
}

export default defineApp({
  id: "[[.ID]]",
  title: "[[.AppTitle]]",
  icon: <ListChecks />,
  home: { view: "list" },
  views: [{ id: "list", title: () => t("[[.Plural]]"), render: () => <List /> }],
  nav: () => [{ label: t("[[.AppTitle]]"), items: [{ label: t("[[.Plural]]"), icon: <ListChecks />, route: { view: "list" } }] }],
});
`

const i18nTS = `// Simplified Chinese for this package (ADR-0023), keyed by the English source text.
import { register } from "@platform/ui";

register("zh-CN", {
  "New": "新建",
  "[[.Plural]]": "[[.Zh]]",
  "Save": "保存",
  "[[.AppTitle]]": "[[.AppZh]]",
});
`
