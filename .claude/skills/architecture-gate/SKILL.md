---
name: architecture-gate
description: Open a platform stage or a structural change with an ADR the owner decides. Use when a stage of docs/Platform.md §10.5 starts, when a work-queue item says "gate", or when a change has lasting cost (a new platform app, a journal entry kind, a kernel rule, a dependency, a public API).
---

# Architecture gate

A gate turns a direction into decisions the owner makes before anything is built. Stages 1–5 (ADR-0016 to ADR-0022) are the examples to follow.

## 1. Establish the facts

- Read `docs/Intent.md`, `docs/Platform.md` §2 (the product model) and §10 (the plan), and the ADRs the change touches.
- Check every claim about the code against the code. Documents state targets; code states facts.
- Look up what the reference platforms (Intent.md, "The comparison") do **now**, from their own documentation or announcements of the current year. Cite what you used. Say when a source could not be read.

## 2. Write `docs/ADR/00NN-<name>.md`

Sections, in this order:
- **Status:** `Proposed (<date>, #<item>)`.
- **Context:** what exists (with code pointers), what is missing, what the reference platforms do (a table), and the few things they agree on.
- **Our constraints:** replay never calls outside; the journal stays small enough to replay; rules and models stay typed code (ADR-0008); no new dependency or download without the owner's approval; no domain vocabulary in `contract/`.
- **Design:** numbered points, each saying who owns what and how it replays.
- **Decision points for the owner:** a table `# | Question | Options | Recommendation`, one row per choice with lasting cost; say what is declined and why.
- **Build items after the decisions:** a table `Batch | Item | Done when`. Every batch's done-when names tests, `CheckReplay`, the rehearsal (`deploy/local/rehearse.sh`) where it applies, and a proof on at least two reference apps from different industries.
- **Consequences.**

## 3. Hand it to the owner

- Add or update the work-queue item with status `gate, owner decision`.
- Stop. Do not build before the owner accepts. Ask only what the decision table leaves open.

## 4. When accepted

- Status becomes `Accepted (<date>, #<item>). The owner accepted D1–Dn as recommended` (or as amended, saying how).
- Build batch by batch; close each with the `close-out` skill.
