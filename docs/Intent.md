# Intent and Product Direction

Durable statement of what the owner wants for the platform. The owner's latest explicit instruction always wins; update this file when direction changes rather than adding new ones.

Provenance: moved from the MSRU repository's `Docs/Intent.md` on 2026-09-25, with the owner's direction of that day added (capability-led, reference-grounded). Music's own product intent stays in MSRU (`Docs/Music.md`, `Docs/AppleClient.md`). The advisory review in [ProductIntentReview.md](ProductIntentReview.md) is handled; what it contributed is in [Platform.md](Platform.md) §1 and §10.

## What we are building

1. **A business platform:** a composable, evolvable platform for building and running business software (Platform.md §1). It has server and edge/client runtimes and is multi-tenant. It supports personal local-first apps and organisational apps where a server is authoritative, up to real operations such as manufacturing plants.
2. **The comparison is with the platforms business software is built on:**
   - Odoo and Frappe;
   - ServiceNow;
   - Salesforce Platform;
   - SAP BTP and CAP;
   - Microsoft Power Platform and Dataverse;
   - Palantir Foundry and AIP.

   The comparison is not with one vertical application. A team should be able to build a CRM, an MES, a PMS or a HIS on it mostly by writing business knowledge.
3. **Music (MSRU)** remains a real product and the long-term validation domain for local-first, personal, edge-side capabilities.

The deepest goal is a platform that **supports change itself**. It must not encode an organisation's or application's current shape as its permanent identity. Domains may be rewritten; the deeper contracts must stay coherent.

## How we decide what the platform has (2026-09-25)

- **Capability-led, grounded in references.** What a business platform must offer is largely known: data models with typed fields and relations, lists and record pages, organisation trees, lifecycles and approvals, task inboxes, workflow orchestration, files, reports and dashboards, integration, AI. Mature platforms show which capabilities recur and how they are shaped. The platform builds these capabilities from that knowledge (Platform.md §10) instead of waiting for one product to pull each of them out.
- **Do not deepen one product.** Industry apps are thin reference apps that exercise capabilities and demonstrate them, several industries side by side. Depth in one vertical is not the goal.
- **Each capability is specified before it is built.** State what the reference systems do, what we adopt, and what we decline and why. Record decisions with lasting cost in an ADR, through an architecture gate when the change is structural. Prove the capability with tests and at least one reference app using it.
- **Tensions between domains are still evidence.** When a reference app needs an exception, the capability is wrong or incomplete; fix the capability, not the app.
- **Kernel stability is judged by evolution.** Domain evolution should mainly change the domain; the kernel changes only for a genuinely missing cross-domain capability.

## What stays true

- **Typed code, not configuration** (AGENTS.md rule 5, Platform.md §6). Capabilities are declared in code and composed by code. Operators configure values, never rules. We borrow what metadata-driven platforms (Odoo, Frappe, Dataverse) achieve — declare once, get list, form, search, API and permissions — without making tenant configuration the programming language.
- **One owner per state, task and resource,** each with a stop condition. Committed work is never reverted by closing what started it.
- **One path per responsibility.** Migrations are closed; no two long-lived paths. Delete old code once it is confirmed unused, and protect existing valid work.
- **Replay is the truth test.** Everything durable enters as a journaled input and rebuilds by replay (ADR-0007); outside calls never happen in replay.
- **AI is an authorized caller.** It sees and does only what its grants allow, and a person approves what cannot be recalled (ADR-0008, ADR-0014 D6).

## Technology direction

- **Go** for the primary backend.
- **Rust** where it gives a real systems or performance advantage.
- **TypeScript, React and `@platform/ui`** for web workspaces.
- **Tauri with Rust** for the cross-platform desktop client.
- **Swift** where Apple-native capabilities and deep local integration matter.

The server must not couple to Apple because the first app was Swift. The kernel is language-neutral contracts plus conformance tests, without four parallel implementations.

## Product quality bar

- Main flows have complete start, progress, completion, failure and recovery behaviour.
- Previews and tests never touch live accounts, network or personal data.
- Refactors change directories, responsibilities and dependencies, not just names.
- Report what was actually verified and what was not; historical passes are not evidence for the current tree. The AI verifies builds, contracts and interfaces; the owner accepts visual quality, feel and end-to-end behaviour.

## Documentation principles

- Repository docs are **durable project knowledge**, not the reasoning that produced it. Keep a small set:
  - AGENTS.md;
  - this file;
  - Platform.md;
  - ADRs for decisions worth preserving;
  - one work queue;
  - client or domain docs where a domain genuinely needs them.
- Consolidate instead of appending. Hypotheses, friction notes and investigations are temporary: fold conclusions into the canonical docs or an ADR, then delete them. Git history is the archive.
- Documents describe targets; code states facts. When they disagree, record the gap.

## Working with AI

- Be concise; reason internally; do not restate requirements or narrate. Read only what the task needs.
- An explicit work request means do it, within the authorized scope. Ask only when:
  - a key input is missing;
  - the ambiguity is material;
  - the action crosses an authorization boundary: publishing, deleting user data, deploying, contacting others, downloading.
- If a plan is unsound, say so directly and propose a better one; do not add complexity to please, and do not silently replace the core goal.
- Written docs go into the repository at the right place; chat reports results and locations.
