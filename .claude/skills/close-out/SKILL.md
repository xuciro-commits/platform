---
name: close-out
description: Finish a batch of platform work — checks, documents and commit together. Use before committing any change that adds or changes a capability, a journal entry kind, an endpoint, a platform app, a reference app or a test route (AGENTS.md rule 8).
---

# Close out a batch

A batch is done when its checks pass and its documents say what now exists, in the same commit.

Read the batch against `docs/Intent.md` → `docs/Platform.md` §10 → `docs/WorkQueue.md`. Keep current capability evidence separate from the target architecture, builder journey and roadmap. A design-only batch may establish an accepted direction; it must not report future runtime or builder capabilities as built.

## 1. Checks

- Run `scripts/verify.sh <step>` for every step the batch touched (see AGENTS.md "Verify"); `capabilities` whenever `capabilities/server` changed, `composition` whenever an app or protocol did, `web` whenever `web/` did.
- A new journal entry kind or field: its row in `docs/Platform.md` §2.7, and `CheckReplay` covering it in a composition's tests.
- For frontend or builder work, verify the complete task named by Platform §10.6 and the work item: representative data, permissions, error/recovery states, visual and keyboard behavior. A component export, generated page or passing unit test alone does not establish product acceptance. Do not claim visual approval the owner has not given.
- For documentation-only changes, check links, references, decision status and consistency with current code; report that runtime tests were not run if they were not applicable. Validate changed skills with the skill-creator validator when available.
- Report exactly what ran and what did not (Swift, Docker, the browser). A pass on an earlier tree is not evidence.

## 2. Documents (one home per fact)

| What changed | Where it goes |
|---|---|
| What was built, where, proven by which tests, what is not yet | The ADR's "As built" |
| A capability now exists or changed | `docs/Platform.md` §2.4 (the capability map); reconcile any conflicting current-state claim, retaining the date and scope of §10.2 audit evidence |
| Product direction, target architecture, builder journey or annual acceptance changed | `docs/Intent.md` and the corresponding part of `docs/Platform.md` §10; an ADR for decisions with lasting cost. Targets remain targets until supported by implementation and verification |
| A promise of an accepted ADR is now built, or newly partial | `docs/Platform.md` §2.9 |
| A new term, or a new owner of data | `docs/Platform.md` §2.5 and §2.3 |
| Something a person can try | A route in `docs/Testing.md` (in Chinese, like the rest of it), walked in the UI first; addresses, accounts and settings in `deploy/local/README.md`. Future acceptance scenarios stay explicitly planned until built and walked |
| A builder can now execute a new authoring or release path | `docs/Apps.md`, with observed prerequisites, commands or UI steps and current limits; keep future paths in Platform §10.4 |
| A new directory, platform app or skill | The map in `AGENTS.md` |
| The item's state | `docs/WorkQueue.md`; delete the item when done |
| Friction found on the way | An `F-n` row in `docs/WorkQueue.md` |
| A type or route the host answers with | `go run ./cmd/api-types` in `capabilities/server` (`TestAPIContract` fails on a stale `host.ts`); a new route is declared with its `Route` |
| A new title, description, choice or UI word | Its Chinese in the app's `i18n/zh-CN.json` or the package's `i18n.ts` (AGENTS.md rule 10; `TestChinese`, `TestLanguages` and the kit's i18n test fail otherwise) |

Then search the docs and agent instructions for the capability's name and fix stale guidance: `rg -n "<name>" docs AGENTS.md .claude/skills deploy/local/README.md`. Preserve historical ADR decisions and mark what supersedes them rather than rewriting history as if the new capability already existed.

## 3. Commit

- One imperative sentence of what changed, with the ADR and item in parentheses (AGENTS.md rule 9).
- Push to the working branch.
- Honor the task's delivery scope: if the owner requested a reviewable document change or explicitly excluded commit/push, leave the changes available for review and report that state.

## 4. Report

What was built; what was verified and how; what was not; which test-guide route the owner should walk.
