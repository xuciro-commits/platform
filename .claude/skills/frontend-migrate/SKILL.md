---
name: frontend-migrate
description: Fixed procedure for an executor model to do exactly one task card of the new frontend (frontend/) from docs/roadmap.md section five. Two kinds of card: declare one menu entry (W7), or finish one presentation that was adapted from an official Astryx template (W11). Use when asked to do a W7 or W11 row, "add the X entry", "declare the X list", or any frontend task that names one app and one entry. Not for changing the shell, the router, the host layer, dependencies, or specs: those belong to the main session.
---

# Frontend task cards: one card, one change

You are an executor. You do **one card**. You do not design, and you do not improve things you were not asked to touch. When something does not fit, you stop and report; you never work around it.

How this frontend is built (read `docs/7-system/web.md`, section "界面运行时", once):

- **Contract**: the shapes the host gives, in `frontend/src/host/`.
- **Presentation**: a component adapted once from an official Astryx template, in `frontend/src/pages/`. It knows the contract, not any one type.
- **Entry**: one declaration in `frontend/src/apps/<app>/<entry>.ts`. The file path is the address `/<app>/<entry>`. Nothing else registers it.

Read these first, in this order, and nothing under `web/`:

1. The task card.
2. `frontend/AGENTS.md`.
3. The example file the card names. Copy its shape.

## The task card

| Field | Meaning |
|---|---|
| 入口 | `<app>/<entry>`, exactly as in `frontend/src/apps.ts` |
| 原型 | the presentation to use, and the example file to copy |
| 数据 | the host type or read path |
| 可改文件 | the only files you may create or change |
| 验收 | what must be true when you finish |

If a field is missing or you do not understand it, stop (see "Stop and report").

## Card kind 1: declare an entry (W7)

The whole entry is one call. For a list of records of one host type:

```ts
import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.connection",
  title: "Connections",
  about: "A connection is an external system this workspace reads from: a kind, an address and a credential reference.",
});
```

- `type` is the host type on the card. Columns and groupings come from the host; do not list them unless the card gives `fields`.
- `title` is the entry's label in `frontend/src/apps.ts`, unchanged.
- `about` is one English sentence saying what this kind of record is. It is shown when the list is empty.
- Add `about` to `zhCN` in `frontend/src/i18n.ts`, with terms from `docs/glossary.md`.

Other declarations exist for other data: `resourceList({ kind, title, about })` (see `src/apps/ontology/object-types.ts`) and `listPage({...})` (see `src/apps/admin/members.ts`). Use the one the card names.

You may touch only: the one new file under `frontend/src/apps/<app>/`, and new lines in `zhCN`.

## Card kind 2: finish a presentation (W11)

The main session has already scaffolded the official template and connected it to the contract. The card lists what is still missing. You may touch only the files the card lists under `frontend/src/pages/` and new lines in `zhCN`.

- Look for an official part before writing anything: `pnpm --dir frontend astryx search "<what you need>"`, then `pnpm --dir frontend astryx template <Block>` or `astryx component <Name>`.
- Keep the template's frame, spacing and interaction. Change only what the card lists.
- Read `astryx component <Name>` before using a prop. Use only props printed there.

## Steps

1. Run `make check-frontend`. If it fails before you changed anything, stop and report.
2. Make the change the card describes, in the files the card allows.
3. Every visible string is English in the source. Add each new one to `zhCN`.
4. Run `make check-frontend`. Fix every message exactly as it says. Repeat until it prints no problems.
5. Look at it with real data: run `make frontend-host` in one terminal, put `PLATFORM_HOST=http://127.0.0.1:18511` in `frontend/.env`, run `pnpm --dir frontend dev`, open `http://localhost:8496/<app>/<entry>`, pick the `admin-1` identity on the sign-in page. Check each line of 验收, in light and dark.
6. Report (format below). Do not commit or push unless the card says to.

## Never

- Never write a raw HTML element (`div`, `span`, `button`, `input`, `table`), a `className`, an inline `style`, a colour, or a pixel size.
- Never write a page from blank, and never write interface code inside an entry file.
- Never invent a component prop, a host path, a type name, a field name, or a menu entry.
- Never add a dependency or import anything outside Astryx, heroicons and `frontend/src`.
- Never call `fetch` yourself.
- Never read or copy anything from `web/`.
- Never write a code comment.
- Never edit `scripts/check-frontend.mjs`, skip the check, or weaken a rule to get past it.
- Never touch `frontend/src/shell/`, `frontend/src/host/`, `frontend/src/router.tsx`, `frontend/src/main.tsx`, `frontend/src/apps.ts`, `frontend/src/apps/entries.ts`, `frontend/src/templates/`, `frontend/package.json`, any lockfile, any config file, `scripts/`, `docs/`, `.claude/`, any `AGENTS.md`, or anything under `web/` and `capabilities/`.
- Never do two cards in one task, and never tidy a file the card did not name.

## Stop and report

Stop at once, change nothing further, and report `BLOCKED` with the exact reason when:

- the example file, the presentation or the host type the card names does not exist;
- the list shows the wrong columns, or the entry needs something the presentation cannot show;
- a component has no prop for what you need, and `astryx search` finds no official part;
- `make check-frontend` fails in a file you were not allowed to touch;
- finishing would need a file on the never-touch list, or a new dependency;
- the card contradicts `docs/4-interaction/README.md` §4.1, or is unclear.

A blocked task that is reported is a good result. A finished task that bent a rule is a bad one.

## Report

```
CARD: <app>/<entry> or <presentation>
STATUS: DONE | BLOCKED
FILES: <each file created or changed>
CHECK: make check-frontend -> <last line it printed>
SEEN: <what you saw in the browser for each 验收 line; say which you could not check>
BLOCKED BY: <only when blocked: what is missing, and where>
```
