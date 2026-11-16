---
name: frontend-migrate
description: Fixed procedure for an executor model to build exactly one menu entry of the new frontend (frontend/) from a task card in docs/roadmap.md. Use when asked to do a W7 row, "migrate an entry", "build the X list / record page", or any frontend task that names one app and one entry. Not for changing the shell, the page archetypes, the router, dependencies, or specs: those belong to the main session.
---

# Frontend migration: one entry, one card

You are an executor. You build **one entry** from **one task card**, by filling in an existing page archetype. You do not design. You do not improve things you were not asked to touch. When something does not fit, you stop and report; you never work around it.

Read these first, in this order, and nothing else under `web/`:

1. The task card (below).
2. `frontend/AGENTS.md`.
3. The archetype file the card names, and one finished entry of the same archetype in `frontend/src/apps/` if there is one. Copy its shape.

## The task card

Every W7 row in `docs/roadmap.md` is a card with these fields. If a field is missing or you do not understand it, stop (see "Stop and report").

| Field | Meaning |
|---|---|
| 入口 | `<app>/<entry>`, exactly as in `frontend/src/apps.ts` |
| 原型 | list, record, canvas or dashboard, and the archetype file to use |
| 数据 | the functions in `frontend/src/host/` to read and write with, and the fields to show |
| 可改文件 | the only files you may create or change |
| 验收 | what must be true when you finish |

## What you may touch

Only the files in the card's 可改文件. In practice that is:

- new files under `frontend/src/apps/<app>/`
- `frontend/src/i18n.ts`: add lines to `zhCN`, never change or remove one

## What you must never touch

`frontend/src/shell/`, `frontend/src/pages/`, `frontend/src/host/`, `frontend/src/router.tsx`, `frontend/src/main.tsx`, `frontend/src/apps.ts`, `frontend/src/templates/`, `frontend/package.json`, any lockfile, `frontend/vite.config.ts`, `frontend/tsconfig.json`, `scripts/`, `docs/`, `.claude/`, any `AGENTS.md`, and everything under `web/` and `capabilities/`.

If the task seems to need a change in one of these, that is a stop condition, not a reason to change it.

## Steps

1. Run `make check-frontend`. If it fails before you changed anything, stop and report.
2. Open the archetype file and the finished example. List what the archetype asks an entry to provide.
3. Create the entry file under `frontend/src/apps/<app>/` by copying the example's shape and replacing its content with what the card says: fields, filters, actions, read and write functions.
4. For every component prop you use, read `pnpm --dir frontend astryx component <Name>` first. Use only props that are printed there.
5. Every visible string is English in the source and passes through `t("...")`. Add each one to `zhCN` in `frontend/src/i18n.ts` with terms from `docs/glossary.md`.
6. Cover the four states the archetype exposes: empty, loading, error, refusal. Do not skip one because the sample data never triggers it.
7. Run `make check-frontend`. Fix every message exactly as it says. Repeat until it prints no problems.
8. Open `http://localhost:8496/<app>/<entry>` (start it with `pnpm --dir frontend dev` if needed) and check each line of the card's 验收 by looking, in light and dark, at desktop and at phone width.
9. Report (format below). Do not commit or push unless the card says to.

## Never

- Never write a raw HTML element (`div`, `span`, `button`, `input`, `table`), a `className`, an inline `style`, a colour, or a pixel size.
- Never invent a component prop, a host function, a field name, or a menu entry.
- Never add a dependency or import anything outside Astryx, heroicons and `frontend/src`.
- Never call `fetch` yourself.
- Never copy code, names, layout or components from `web/`.
- Never write a code comment.
- Never edit `scripts/check-frontend.mjs`, skip the check, or weaken a rule to get past it.
- Never change two entries in one task, and never "tidy up" a file the card did not name.

## Stop and report

Stop at once, change nothing further, and report `BLOCKED` with the exact reason when:

- the archetype file or the host function the card names does not exist;
- the archetype cannot express something the card asks for (a column type, a filter, an action, a layout);
- a component has no prop for what you need;
- `make check-frontend` fails in a file you were not allowed to touch;
- finishing would need a file on the never-touch list, or a new dependency;
- the card contradicts `docs/4-interaction/README.md` §4.1, or is unclear.

A blocked task that is reported is a good result. A finished task that bent a rule is a bad one.

## Report

```
ENTRY: <app>/<entry>
STATUS: DONE | BLOCKED
FILES: <each file created or changed>
CHECK: make check-frontend -> <last line it printed>
SEEN: <what you saw in the browser for each 验收 line; say which you could not check>
BLOCKED BY: <only when blocked: what is missing, and where>
```
