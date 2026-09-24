# AGENTS.md

Start here (Claude Code reaches this file through `CLAUDE.md`).

## What this repository is

The **business platform**: the kernel contract, and later the Go backend, reference-domain slices and platform-level Rust components (ADR-0003). It is multi-tenant and survives domain change; applications (Music, Hotel, manufacturing) validate it, they are not its source of truth. The goals and collaboration rules are in the MSRU repository's `Docs/Intent.md`; Apple client code and the Music product live there.

## Map

| Path | Contents |
|---|---|
| `contract/` | Kernel contract `v1alpha1`: Protobuf data contract (`proto/`), semantic rules with errors (`spec/`), conformance vectors (`vectors/`), Go reference (`go/`) and Swift implementation (`swift/`) running the same vectors |
| `slices/hotel/` | Hotel reference slice (#79): Go tenant server (`server/`, with a channel simulator), Tauri desk client with a Rust K5 outbox that runs the contract's K5 vectors (`client/`), `flows.sh` reproducing timeout, offline, conflict and rejection flows. May not change the kernel |
| `web/` | pnpm workspace: `packages/ui` (`@platform/ui`, the shared UI kit, ADR-0004), `apps/gallery` (every component with cross-industry data), `apps/hotel-desk` (the Hotel slice's client UI, loaded by Tauri) |
| `docs/Platform.md` | The platform design: layers, kernel hypotheses K1–K9, kernel contract rules, validation strategy |
| `docs/WorkQueue.md` | The only active plan and the open friction list |
| `docs/ADR/` | Decisions with lasting cost |
| `scripts/verify.sh` | All checks |

## Rules

1. The kernel is a language-neutral contract (ADR-0002): schema, semantics, errors, compatibility, conformance and scope are separate parts; Protobuf never defines meaning.
2. No domain vocabulary in `contract/` (checked). Domain slices may not change the kernel; they record friction in the work queue.
3. Kernel contract changes: spec rule and vectors first, then Go and Swift; list breaking changes while the version is `v1alpha1`, and write an ADR once it is `v1`.
4. Least code: delete over add; no shims as an end state; generated code (`contract/go/gen`) is never edited by hand.
5. Clients use `@platform/ui`; no per-slice HTML or second component library. Components are composed in typed code, never driven by configuration.
6. Repository docs are durable knowledge; plans and summaries go to the work queue or chat, not new files.

## Verify

```sh
scripts/verify.sh           # everything
scripts/verify.sh contract  # contract vocabulary, buf lint + generated code, Go vet/test, Swift test
scripts/verify.sh web       # UI kit tests, typecheck and build of every web app (needs node, pnpm)
scripts/verify.sh hotel     # web build, Hotel server tests, Rust K5 vectors, end-to-end flows (needs cargo)
```
