---
name: platform-batch
description: The delivery loop for one batch of platform work, from choosing it on the roadmap to pushing it to main. Use when starting a batch, when the owner says "next batch", "continue", or names a roadmap item, and before committing a batch.
---

# Platform batch

A batch is one thin, usable slice across layers. It ends committed and pushed, with the specs and roadmap telling the truth.

## 1 Choose

- Read `docs/roadmap.md`. Take the item under "下一批" unless the owner named another. The owner sets direction, not order; ask only about decisions with a lasting cost.
- Confirm the item's owner in `docs/2-product.md`. If there is none, the batch starts by fixing that spec.

## 2 Read

- Read only the spec sections the item links to, plus the rules for the paths you will touch (`.claude/rules/` loads them).
- Write down which spec statements the batch will make true, and which, if any, it must change.

## 3 Plan

- Multi-file or cross-layer work: plan first (plan mode), listing files per step.
- Split into briefs that touch disjoint files. A brief for `implementer` contains:
  - goal and the spec sections it implements (paths with § numbers);
  - files in scope, and files explicitly out of scope;
  - paths to delete;
  - checks to run;
  - what to report back.
- Gather facts with Explore (Haiku for lookups, Sonnet for reading code); keep design decisions in the lead.

## 4 Build

- Delete what the change replaces in the same batch.
- Fix small, clearly wrong things in passing (a stale line, a missing translation, a dead entry). Anything that needs its own design goes to the roadmap instead.
- Never write code comments. Tests only as kernel invariants or golden-path acceptance (docs/7-system/verification).

## 5 Verify

- Run the checks in docs/7-system/verification §7.8 for what changed, plus any acceptance spec on the touched path.
- For large or risky diffs, ask `reviewer` for an independent pass against the specs and fix its confirmed findings.
- The commit hook re-runs build, types and whitespace, and lists specs whose `covers` match the change. Update them, or re-run the same commit if they are still true.

## 6 Close

- In the specs: delete gap entries the batch closed; add gaps it found; change rules it overturned.
- In the roadmap: delete finished rows, update "下一批", and add newly found work under its phase.
- Commit message: Chinese subject naming the batch, a blank line, a body of what changed and why, then the attribution line. Push to main.
- Report to the owner: what is now true, the screens to walk, and anything not verified (say exactly what was not run).
