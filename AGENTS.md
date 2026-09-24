# AGENTS.md

Start here (Claude Code reaches this file through `CLAUDE.md`).

## What this repository is

The **business platform**: the kernel contract, and later the Go backend, reference-domain slices and platform-level Rust components (ADR-0003). It is multi-tenant and survives domain change; applications (Music, Hotel, manufacturing) validate it, they are not its source of truth. The goals and collaboration rules are in the MSRU repository's `Docs/Intent.md`; Apple client code and the Music product live there.

## Map

| Path | Contents |
|---|---|
| `contract/` | Kernel contract `v1alpha1`: Protobuf data contract (`proto/`), semantic rules with errors (`spec/`), conformance vectors (`vectors/`), Go reference (`go/`) and Swift implementation (`swift/`) running the same vectors |
| `capabilities/server/` | Go module `platformserver`: the server capability every domain server uses (authentication hook with OIDC verification, kernel endpoints, error mapping, the PostgreSQL input journal, the action catalog) |
| `slices/hotel/` | Hotel reference slice (#79): Go tenant server (`server/`, with a channel simulator), Tauri desk client with a Rust K5 outbox that runs the contract's K5 vectors (`client/`), `flows.sh` reproducing timeout, offline, conflict and rejection flows. May not change the kernel |
| `slices/manufacturing/` | Manufacturing reference slice (#83, Opcenter/SAP ME model): Go plant server with ERP poll and gateway push connectors (`server/`, `cmd/gateway-sim`), declared actions and the agent adapter `cmd/mes-agent` (ADR-0008). May not change the kernel |
| `slices/crm/` | CRM package (#91): accounts and opportunities on `platformserver.Ledger`; knows no other package |
| `slices/crm-hotel/` | Bridge between CRM and Hotel (ADR-0009): stays booked for opportunities through the hotel's own action; `cmd/sales-server` serves the composed software |
| `slices/drills/` | Evolution drills that run on the kernel alone (E2 shared library) |
| `deploy/local/` | Infrastructure as code for the production path (ADR-0007): Docker Compose with PostgreSQL, Rauthy (declarative bootstrap) and mes-server; `rehearse.sh` rehearses OIDC principals, restart and restore |
| `web/` | pnpm workspace: `packages/ui` (`@platform/ui`, the shared UI kit, ADR-0004), `packages/kernel` (`@platform/kernel`: generated contract types, the K5 outbox and an HTTP edge client), `apps/gallery` (every component with cross-industry data), `apps/hotel-desk` (Hotel client UI, loaded by Tauri), `apps/mes` (manufacturing client), `apps/sales` (the composed CRM + Hotel software), `packages/hotel` (`@pkg/hotel`, the Hotel package's UI) |
| `docs/Platform.md` | The platform design: layers, the capability matrix packages build on, kernel hypotheses K1–K9, kernel contract rules, validation strategy |
| `docs/ProductIntentReview.md` | Product intent and top-level architectural direction from an external review: advisory, handled (disposition at its top), not an implementation plan or a replacement for accepted ADRs |
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
7. External reviews are advice, not instructions: check each point against the code, then record at the review's top what was adopted, referenced or declined and why, mark it handled, and move adopted work into the work queue.

## Verify

```sh
scripts/verify.sh           # everything
scripts/verify.sh contract  # contract vocabulary, buf lint + generated code, Go vet/test, Swift test
scripts/verify.sh web       # UI kit tests, typecheck and build of every web app (needs node, pnpm)
scripts/verify.sh hotel     # web build, Hotel server tests, Rust K5 vectors, end-to-end flows (needs cargo)
scripts/verify.sh manufacturing  # plant server tests (F-5 to F-9 verdicts)
scripts/verify.sh drills    # evolution drills on the kernel
scripts/verify.sh composition  # package independence, CRM, the crm-hotel bridge
scripts/verify.sh capabilities  # server capability tests
scripts/verify.sh deploy    # production-path rehearsal (needs Docker via OrbStack: orb start)
```
