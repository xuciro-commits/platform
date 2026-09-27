---
name: architecture-gate
description: Open a platform stage or a structural change with an ADR the owner decides. Use when a stage of docs/Platform.md §10.5 starts, when a work-queue item says "gate", or when a change has lasting cost (a new platform app, a journal entry kind, a kernel rule, a dependency, a public API).
---

# Architecture gate

A gate turns the accepted direction into the remaining structural decisions before implementation. ADR-0031 establishes the next-stage product direction; Platform §10 owns its target design and annual roadmap. Existing owner authorization remains valid: do not reopen a settled decision merely because this skill is invoked.

## 1. Establish the facts

- Read `docs/Intent.md` → `docs/Platform.md` §10 → `docs/WorkQueue.md`, then §2 (the current product model) and the ADRs the change touches.
- Check every implementation claim against the current code and tests. Distinguish the dated audit (§10.2), target architecture and builder journey (§10.3–10.4), roadmap (§10.5) and acceptance (§10.6). An accepted design does not prove its implementation.
- Look up what the reference platforms (Intent.md and Platform §10.1) do **now**, from their own documentation or announcements. Cite the exact sources and distinguish current product claims from stable engineering principles. Say when a source could not be read.

## 2. Write `docs/ADR/00NN-<name>.md`

Sections, in this order:
- **Status:** `Proposed (<date>, #<item>)` for undecided choices; when the owner has already authorized the concrete direction, record that acceptance and its scope instead of asking again.
- **Context:** what exists (with code pointers), what is missing, what the reference platforms do (a table), and the few things they agree on.
- **Our constraints:** replay never calls outside; the journal stays small enough to replay; code and controlled, typed definitions share semantic validation, permissions and release controls (ADR-0031); no arbitrary tenant code execution is promised; respect the owner's existing authorization for dependencies and downloads; no domain vocabulary in `contract/`.
- **Design:** numbered points, each saying who owns what, which builder and operator task it enables, and how it replays or recovers. Frontend and application-building capabilities belong on the mainline; give them interaction and usability criteria as well as runtime criteria.
- **Decision points for the owner:** a table `# | Question | Options | Recommendation`, one row per choice with lasting cost; say what is declined and why.
- **Build items after the decisions:** a table `Batch | Item | Done when`. Every batch's done-when names applicable checks, `CheckReplay` and the rehearsal (`deploy/local/rehearse.sh`) for persistence changes, and the complete builder/operator journey proving it. Shared capabilities need proof on at least two reference apps from different industries; frontend work needs observed visual and interaction acceptance, not only generated screens.
- **Consequences.**

## 3. Hand it to the owner

- For unresolved choices with lasting cost, add or update the work-queue item with status `gate, owner decision`. Explain which decision remains open and ask only for that decision before its dependent implementation.
- When the owner has already accepted the choices, record that evidence and continue within the authorized scope. Authorization to establish a design document does not by itself authorize implementing all its future capabilities.

## 4. When accepted

- Status becomes `Accepted (<date>, #<item>). The owner accepted D1–Dn as recommended` (or as amended, saying how).
- Build batch by batch; close each with the `close-out` skill.
