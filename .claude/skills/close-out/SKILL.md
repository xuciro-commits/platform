---
name: close-out
description: Finish a batch of platform work — checks, documents and commit together. Use before committing any change that adds or changes a capability, a journal entry kind, an endpoint, a platform app, a reference app or a test route (AGENTS.md rule 8).
---

# Close out a batch

A batch is done when its checks pass and its documents say what now exists, in the same commit.

## 1. Checks

- Run `scripts/verify.sh <step>` for every step the batch touched (see AGENTS.md "Verify"); `capabilities` whenever `capabilities/server` changed, `composition` whenever an app or protocol did, `web` whenever `web/` did.
- A new journal entry kind or field: its row in `docs/Platform.md` §2.7, and `CheckReplay` covering it in a composition's tests.
- Report exactly what ran and what did not (Swift, Docker, the browser). A pass on an earlier tree is not evidence.

## 2. Documents (one home per fact)

| What changed | Where it goes |
|---|---|
| What was built, where, proven by which tests, what is not yet | The ADR's "As built" |
| A capability now exists or changed | `docs/Platform.md` §2.4 (the capability map), and its row leaves §10.4 |
| A promise of an accepted ADR is now built, or newly partial | `docs/Platform.md` §2.9 |
| A new term, or a new owner of data | `docs/Platform.md` §2.5 and §2.3 |
| Something a person can try | A route in `deploy/local/README.md` (in Chinese, like the rest of it) |
| A new directory, platform app or skill | The map in `AGENTS.md` |
| The item's state | `docs/WorkQueue.md`; delete the item when done |
| Friction found on the way | An `F-n` row in `docs/WorkQueue.md` |
| A type or route the host answers with | `go run ./cmd/api-types` in `capabilities/server` (`TestAPIContract` fails on a stale `host.ts`); a new route is declared with its `Route` |
| A new title, description, choice or UI word | Its Chinese in the app's `i18n/zh-CN.json` or the package's `i18n.ts` (AGENTS.md rule 10; `TestChinese`, `TestLanguages` and the kit's i18n test fail otherwise) |

Then search the docs for the capability's name and fix every stale mention: `grep -rn "<name>" docs AGENTS.md deploy/local/README.md`.

## 3. Commit

- One imperative sentence of what changed, with the ADR and item in parentheses (AGENTS.md rule 9).
- Push to the working branch.

## 4. Report

What was built; what was verified and how; what was not; which test-guide route the owner should walk.
