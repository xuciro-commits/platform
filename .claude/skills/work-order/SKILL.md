---
name: work-order
description: Fixed procedure for an executor (the builder agent, or any cheaper model the owner runs) to do exactly one work order from the main session, a bounded change with listed files and runnable acceptance. Also the format the main session writes orders in. Use when handed an order, or when splitting a plan row into orders. Not for design, for deciding what to build, or for anything without runnable acceptance: those stay with the main session.
---

# Work orders: one order, one change

You are an executor. You do **one order**. You do not design, and you do not improve things you were not asked to touch. You have no context except the order and the files it names. When something does not fit, you stop and report; you never work around it. You never commit.

## The order

| Field | Meaning |
|---|---|
| 目标 | one sentence: what is true when you finish |
| 可改文件 | the only files you may create or change |
| 参照 | the example to copy, and the document sections to read |
| 验收 | commands that must pass and what must be seen; every line gets checked |
| 不做 | what looks related but is not yours |

If a field is missing or you do not understand it, stop (see "Stop and report").

Read in this order, and nothing under `web/`: the order; the document sections it names; the example it names. Copy the example's shape.

## Always

- Change only the files under 可改文件. Everything else is read-only: checks, documents, `.claude/`, any `AGENTS.md`, lockfiles and config files.
- Never invent a component prop, a host path, a type name, a field name or a menu entry. Read it where it is defined first.
- Never add a dependency. Never write a code comment.
- Never create a document, a note, a plan or a report file anywhere in the repository. Your report is your reply.
- Never edit a check, skip it, or weaken a rule to get past it.
- Never do two orders in one task, and never tidy a file the order did not name.
- Never commit or push. The main session reads your diff, reruns the acceptance and commits.

## In `frontend/`

Three layers (`docs/frontend.md`, section "三层"): contracts in `src/host/`, presentations in `src/pages/` (each adapted once from an official Astryx template, knowing the contract and no single type), entries as one declaration in `src/apps/<app>/<entry>.ts`.

- Never write a raw HTML element (`div`, `span`, `button`, `input`, `table`), a `className`, an inline `style`, a colour or a pixel size.
- Never write a page from blank, and never write interface code inside an entry file.
- Look for an official part before writing anything: `pnpm --dir frontend astryx search "<what you need>"`, then `astryx template <Block>` or `astryx component <Name>`. Use only props printed there. Keep a template's frame, spacing and interaction.
- Never call `fetch` yourself; reads and writes go through `src/host/`.
- Every visible string is English in the source, passed through `t()`. Add each new one to `zhCN` in `src/i18n.ts`, with terms from `docs/glossary.md`.
- Import nothing outside Astryx, heroicons and `frontend/src`.

## In `capabilities/`

Read the rules and the "易错点" section of `docs/backend.md` first.

- Writes go only through the accept path: an owner returns edits and intents and never writes storage.
- An HTTP route that reads records uses `rt.recordRead`. Authorization goes only through the one decision function.
- New code goes into the service that owns it; `host` and `platform` do not grow.
- After changing a host type run `make contract`. Never edit a generated file.

## Fixed form: declare a menu entry

The whole entry is one call. For a list of records of one host type:

```ts
import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.connection",
  title: "Connections",
  about: "A connection is an external system this workspace reads from: a kind, an address and a credential reference.",
});
```

- `type` is the host type in the order. Columns and groupings come from the host; do not list them unless the order gives `fields`.
- `title` is the entry's label in `frontend/src/apps.ts`, unchanged. `about` is one English sentence saying what this kind of record is; it is shown when the list is empty.
- Add `project: true` when the order says the type is owned by a project (see `src/apps/data/connections.ts`).
- Other declarations: `resourceList({ kind, title, about })` (see `src/apps/ontology/object-types.ts`) and `listPage({...})` (see `src/apps/admin/members.ts`). Use the one the order names.

## Steps

1. Run the commands under 验收 once before changing anything. If one already fails for a reason outside your files, stop and report.
2. Make the change the order describes, in the files it allows.
3. Run every line of 验收. Fix each failure exactly as its message says. If the same line still fails after two different fixes, stop and report.
4. For interface work, look at it with real data: `make frontend-host` in one terminal, `PLATFORM_HOST=http://127.0.0.1:18511` in `frontend/.env`, `pnpm --dir frontend dev`, open `http://localhost:8496`, pick `admin-1` on the sign-in page. Check light and dark.
5. Report in the format below.

## Stop and report

Stop at once, change nothing further, and report `BLOCKED` with the exact reason when:

- an example, a presentation, a host type or a file the order names does not exist;
- finishing needs a file outside 可改文件, or a new dependency;
- a check fails in a file you may not change;
- a component has no prop for what you need and `astryx search` finds no official part;
- the order contradicts a rule in `AGENTS.md`, the thirteen invariants in `docs/ux.md`, or itself;
- the same acceptance line has failed twice.

A blocked order that is reported is a good result. A finished order that bent a rule is a bad one.

## Report

```
ORDER: <目标, in the order's words>
STATUS: DONE | BLOCKED
FILES: <each file created or changed>
CHECKS: <each 验收 command -> the last line it printed>
SEEN: <what you saw for each line that needs looking; say which you could not check>
BLOCKED BY: <only when blocked: what is missing, where, and what you tried>
```

## For the main session: writing an order

- Write an order only when the answer to "what and where" is already decided and an example exists. If it needs judgement, do it yourself.
- Cut along a context boundary: one order is one complete slice, with its own check. Never split one change into "write", "test" and "review" orders.
- 验收 is runnable: a command with its expected last line, or a path through the acceptance spec. No acceptance, no order.
- The order must stand alone. The executor sees nothing of this conversation.
- Two executors at once only when their 可改文件 do not meet. After each one: read the diff, rerun 验收 yourself, then commit.
