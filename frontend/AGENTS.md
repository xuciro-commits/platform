# AGENTS

The platform's frontend, rebuilt from zero on Astryx. The repository's root `AGENTS.md` still applies; this file adds what is specific to this folder.

- Every screen holds the thirteen invariants in `docs/4-interaction/README.md` §4.1 and is one of four archetypes (§4.3). Use the `platform-design` skill before building.
- `web/` is the previous frontend. Do not read it for structure, naming or components.
- Run `pnpm dev` (http://localhost:8496) and `pnpm astryx <cmd>` for the CLI below.
- Every visible string goes through `t()` with a `zh-CN` entry in `src/i18n.ts`. No code comments.
- Check with `make check-frontend` from the repository root. Fix what it reports; never edit the check.
- Building one menu entry from a task card? Follow `.claude/skills/frontend-migrate/SKILL.md` to the letter: you may only add files under `src/apps/<app>/` and lines to `src/i18n.ts`. The shell, router, `src/pages/`, `src/host/`, dependencies and specs are not yours to change; when the task needs them, stop and report.
- The block below is generated; refresh it with `pnpm astryx init --agent-docs-path AGENTS.md`, never by hand.

<!-- ASTRYX:START -->
Astryx v0.7.16 · 174 components
CLI: run every command as `pnpm exec astryx <cmd>` (shown below as `astryx ...`).

SETUP (once, in app entry e.g. main.tsx) — without these, components render unstyled:
  import "@astryxdesign/core/reset.css";
  import "@astryxdesign/core/astryx.css";

WORKFLOW — start every page from a template, never from scratch:
1. `astryx build "<idea>"` — names the template to scaffold from and the parts it lacks.
2. `astryx template <name> <path>` — scaffold it; keep its frame, gap and padding; replace the content.
3. `astryx template <Block>` for parts it lacks; `astryx component <Name>` to read props before using one.
Changing an existing page? Keep it: skip step 2 and add blocks/components inside its sections.
Before you deliver a build, run `astryx doctor --built <dir>`; don't deliver it if it fails.

RULES:
- No <div> — components handle all layout and spacing.
- Read `astryx docs layout` before changing a template's frame.
- Dense data = rows (Table, List/Item), not Card-wrapped lists. Badge = counts only.
- Style with component props first, then the xstyle prop / StyleX tokens. No raw hex/px.
- THEMES: palettes and custom colors go through the theme, never :root overrides. To make or change a theme (colors, fonts, corners, spacing, a look from a picture), prefer the experimental theme editor over hand-written theme files: `astryx experimental theme editor new <name> --like neutral`, then `editor set <name> <thing> <value>` or `editor how "<what you want>"`; wire it with `astryx theme add <name>`; after each change look at it with `astryx experimental theme editor show <name> --png` and open the PNGs it writes (changes nothing in the app); a theme change is not done until you have looked at the pictures. In code, extend an imported theme to customize it; `theme eject <slug>` only forks source. See `astryx docs theme`.
- SELF-CHECK: re-read the file; replace any className=, style={{…}}, raw <div>/<span>, imported .css/@apply, or hardcoded #hex/px with the component or the xstyle prop + a token.

`astryx help` lists every command. Key ones beyond the workflow:
  search "<query>"   find any component / hook / doc / template
  discover <words>   integrations you could add
  docs <topic>       getting-started, principles, tokens, theme … (`astryx docs` lists all)
  docs cli           commands and API reference
  docs authoring     build an integration package, and every field it holds
  upgrade --from <old version> --apply   run after a dependency bump
<!-- ASTRYX:END -->
