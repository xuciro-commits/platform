# Platform

The **AI business application platform**: kernel contract (`contract/`), Go host runtime (`capabilities/server/`), web workspaces (`web/`), reference apps (`apps/`), and cross-industry solutions (`solutions/`). Multi-tenant and surviving domain evolution (ADR-0003, ADR-0025). Its next stage equips FDEs and customers to grow industry applications through shared semantics, governed AI and code or controlled visual composition (ADR-0031); today's authoring path remains typed Go and TypeScript.

The primary business target apps are **CRM, MES, and ERP**, with PMS, HCM, and CSM serving as reference apps.

## Documentation Index

- **For AI Coding Assistants:** Start at [AGENTS.md](AGENTS.md) (Claude Code reaches this via [CLAUDE.md](CLAUDE.md)).
- **Local Environment & Running:** Start at [deploy/local/README.md](deploy/local/README.md) for Docker Compose, Rauthy OIDC, accounts, and server endpoints.
- **Product Intent & Rules:** [docs/Intent.md](docs/Intent.md) (capability-led, reference-grounded, apps as probes).
- **Architecture & Capability Map:** [docs/Platform.md](docs/Platform.md) (current layers, replay semantics and hypotheses K1–K9; §10 is the authoritative next-stage design, dated audit, annual roadmap and acceptance criteria).
- **Testing Scenarios:** [docs/Testing.md](docs/Testing.md) (platform capability guarantees tested across business scenarios).
- **Active Plan & Friction:** [docs/WorkQueue.md](docs/WorkQueue.md) (the only active task list).
- **Architecture Decisions:** [docs/ADR/](docs/ADR/) (decisions with lasting cost; [ADR-0031](docs/ADR/0031-ai-application-platform.md) establishes the AI application platform direction).
- **App Authoring Guide:** [docs/Apps.md](docs/Apps.md) (today's scaffold, entities, actions, flows, translations and run; the boundary with future FDE/customer building tools).
- **Kernel Contract:** [contract/spec/](contract/spec/) (K1–K9 semantics, error codes, conformance vectors).

## Quick Verification

Choose work by reading Intent → Platform §10 → WorkQueue. Current capability evidence, target design and task completion have separate homes; a planned builder is not an available product feature.

```sh
scripts/verify.sh ci          # Continuous integration suite
scripts/verify.sh web         # Web workspaces and UI tests
scripts/verify.sh composition # App boundaries and layout
```
