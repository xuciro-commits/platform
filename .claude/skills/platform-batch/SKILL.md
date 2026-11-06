---
name: platform-batch
description: The delivery loop for one batch of platform work, from choosing it on the roadmap to pushing it to main. Use when starting a batch, when the owner says "next batch", "continue", or names a roadmap item, and before committing a batch.
---

# Platform batch

A batch is one thin, usable slice across layers. It ends committed and pushed, with the specs and roadmap telling the truth.

## 1 Choose

- Read `docs/roadmap.md`. Take the item under "当前批次" unless the owner named another; when it is empty, the owner picks the next one. The owner sets direction, not order; ask only about decisions with a lasting cost.
- Confirm the item's owner in `docs/2-product.md`. If there is none, the batch starts by fixing that spec.

## 2 Read

- Read only the spec sections the item links to, plus the rules for the paths you will touch (`.claude/rules/` loads them).
- Write down which spec statements the batch will make true, and which, if any, it must change.

## 3 Plan

- Multi-file or cross-layer work: plan first (plan mode), listing files per step.
- Record the batch's scope:
  - goal and the spec sections it implements (paths with § numbers);
  - files in scope, and files explicitly out of scope;
  - paths to delete;
  - checks to run;
  - what to report back.

## 4 Build

- Delete what the change replaces in the same batch.
- Fix small, clearly wrong things in passing (a stale line, a missing translation, a dead entry). Anything that needs its own design goes to the roadmap instead.
- A new Go import outside docs/7-system/README §7.2 is fixed in the code, or the table is changed as a design decision stated in the commit body. Never add a gap entry to make the check pass.
- Never write code comments. Tests only as kernel invariants or golden-path acceptance (docs/7-system/verification).

## 5 Verify

- Run `make check` (under a minute; the commit hook runs it too) and `make e2e SPEC=…` for the acceptance specs on the touched path. Run `make verify` when the change touches the shared shell, host composition, contracts or several task paths (docs/7-system/verification §7.12).
- For large or risky diffs, review the changes against the specs and fix confirmed findings.
- The commit hook runs `make check`, and lists specs whose `covers` match the change. Update them, or re-run the same commit if they are still true.

## 6 Close

- In the specs: delete gap entries the batch closed; add gaps it found; change rules it overturned.
- In the roadmap: delete finished rows, keep "当前批次" truthful, and put newly found unscheduled work under "已知漂移".
- Commit message: Chinese subject naming the batch, a blank line, a body of what changed and why, then the attribution line. Push to main.
- Report to the owner: what is now true, the screens to walk, and anything not verified (say exactly what was not run).
