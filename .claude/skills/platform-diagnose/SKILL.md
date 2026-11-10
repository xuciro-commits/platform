---
name: platform-diagnose
description: Architecture diagnosis for the platform repo - find where the code is over-defended, shallow, duplicated, misnamed or misplaced, prove each finding, and turn it into spec gaps, roadmap rows and work orders for the implementer. Use when the owner asks for an architecture review, a health check, "what is wrong with X", or says the code feels bloated or confusing; at the end of each milestone; and before writing a work order for a structural change.
---

# Platform diagnose

The lead diagnoses and prescribes; the implementer treats. A diagnosis ends as changes to specs and the roadmap plus work orders, never as a report file. The principles behind it are `docs/method.md` R3-R9.

The method borrows from two upstream skills installed at user level: `improve-codebase-architecture` and `codebase-design` (mattpocock/skills) for the vocabulary and the deletion test, and `ponytail-audit` (DietrichGebert/ponytail) for the lean categories and the evidence rule. Where they disagree with this repo, the repo wins: no ADR files, no `GLOSSARY.md` (the glossary is `docs/glossary.md`), no HTML report, no code comments, no unit tests.

## 1 Scope

Do not survey the whole tree. Pick the area from evidence:

- The owner's complaint, or the roadmap stage in progress.
- Hot spots: `git log --since="3 months ago" --name-only --format= -- capabilities/server | sort | uniq -c | sort -rn | head -40`. Files changed most are where friction costs most.
- The ratchet line printed by `node scripts/check-boundaries.mjs`, and the gap sections of `docs/7-system/README.md` and `docs/6-domain/README.md`.

Read the spec sections that own the area first. A finding is a distance between the code and the spec, or a defect in the spec itself; say which.

Broad reading goes to a read-only subagent with a precise question and a required answer shape (paths with line numbers, counts with the command that produced them). Read the load-bearing files yourself: `kernel/`, the accept path, anything the finding will rest on.

## 2 Vocabulary

Use these words exactly, so findings and work orders stay comparable.

| Word | Meaning |
|---|---|
| module | anything with an interface and an implementation: a function, a type, a package |
| interface | everything a caller must know: signatures, ordering, error modes, invariants |
| depth | behaviour behind the interface divided by the size of the interface; deep is good |
| seam | a place where behaviour can change without editing the caller |
| adapter | one concrete thing behind a seam |
| leverage | what callers get from depth |
| locality | a change, a bug or a piece of knowledge lives in one place |

"Service" (服务) is a glossary term for a Go package that owns records; do not use it loosely. Domain words come from `docs/glossary.md`.

## 3 What to look for

| Symptom | Test | Usual prescription |
|---|---|---|
| Shallow module | Deletion test: remove it in your head. Complexity vanishes: it was a pass-through. Complexity spreads to N callers: it earns its keep. | inline and delete, or merge into the module it forwards to |
| No locality | One concept needs edits in several packages; the same check appears at several call sites | one owner, one function; call sites lose their copy |
| Hypothetical seam | An interface with one implementation; `x, ok := v.(I)` capability probes; variadic options nobody sets; a config key with one value | delete the seam, call the concrete type; a seam is real when it has two adapters |
| Defence inside the boundary | Name the external input that reaches this branch. Cannot: it is dead defence. Validation repeated after the boundary already parsed; errors swallowed into defaults; nil checks on values the constructor guarantees | delete; keep validation at the trust boundaries listed in method R6, and failure at the kernel's single place |
| Two models, two paths | Two types for one concept; a second write path; a "legacy", "compat" or "v0" branch beside the current one | keep the one the spec names, migrate callers, delete the other (check the format epoch, 7-system §7.6) |
| Words against the glossary | A public identifier uses a word the glossary defines differently, or a concept has two names | rename to the glossary word; a pure rename is its own batch |
| Misplaced code | Code sits in a package whose §7.2 row does not own it; a package name differs from its directory | move to the owner; the boundary check proves it |
| Lean | delete (no caller), reuse (duplicates something in the tree), merge (two things that always change together), split (one thing with two reasons to change) | the smallest change that removes it, deletion first |

Do not report style, naming taste, or anything a formatter or `go vet` already enforces.

## 4 Evidence

- No case, no finding. Every finding names file and line, and either a count with its command or a concrete scenario.
- Before calling something unused, search the whole tree: Go, `web/`, `web/e2e/`, `deploy/packages/`, `scripts/`, and reflection or string-keyed registries.
- Before calling a branch dead defence, trace its callers to a trust boundary and say which input cannot arrive.
- Mark what you inferred but did not check as 待核对. Do not round an estimate into a fact.
- Rank by leverage: lines and concepts removed, and how many later changes it unblocks, against risk to K1, K2, K3 and K13.

## 5 Prescribe

Each finding gets exactly one outcome:

| Outcome | When | Where it goes |
|---|---|---|
| Fix in passing | a few lines, no design choice | the current batch |
| Gap entry and roadmap row | the spec is right, the code is not | the owning spec's 现存差距; `docs/roadmap.md` |
| Spec change | the spec is wrong or silent | the spec's rules, in the same batch as the first code that follows it |
| Owner decision | it touches a load-bearing decision (1-enterprise §1.10) or the list in method "何时问负责人" | ask, with a recommendation and what each option costs later |
| Ratchet | the same violation has appeared twice | a metric in `scripts/shape-baseline.json` or a check in `scripts/` |
| Drop | real but not worth its risk now | nowhere; say so in the session |

When a prescription changes a public interface or kernel semantics, design it twice: write two interfaces that differ in kind (not in detail), compare them by what callers must know, pick one. Do this yourself; do not delegate it.

## 6 Work order

Delegated work goes to the `implementer` agent (Sonnet, local checkout, no worktree) as one message with this shape. An order is ready when the file lists are complete and "Done when" is commands.

```
Goal: one sentence, and the spec section it makes true (path §n).
Read first: the exact files and spec sections, nothing more.
In scope: files to change.
Out of scope: files that must not change, even if they look wrong.
Delete: paths, types and branches that must be gone at the end.
Done when: commands and their expected results (make check; the grep that must return nothing; the ratchet value).
Do not: add comments, tests, dependencies, interfaces, options or compatibility branches; change any spec rule; commit.
Stop and report if: the plan contradicts the code or a spec; a kernel invariant assertion would need to change; the change cannot stay inside scope.
Report: files changed; each command run with its real output; what was not verified; anything noticed outside scope.
```

Keep orders small enough to review by reading the diff. Orders with disjoint file sets may run in parallel. Kernel semantics, concurrency, storage format and spec rules are not delegated (method "亲手做还是派出去").

## 7 Review and close

- Read the whole diff. Kernel paths line by line; mechanical moves by sampling plus the checks.
- Confirm "Delete" happened: search for what should be gone.
- Reject additions the order did not ask for: new helpers, options, guards, fallbacks.
- Run the "Done when" commands yourself; the implementer's report is a claim, not a result.
- Lower the baselines in `scripts/shape-baseline.json` that the change improved.
- Close as a batch (`platform-batch` §6): delete the gap entries and roadmap rows that are now false, add the ones found, commit, push.
- Tell the owner what is now true, what was not verified, and which findings wait on a decision.
