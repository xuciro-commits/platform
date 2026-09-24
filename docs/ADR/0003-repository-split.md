# ADR-0003: Separate platform repository, after the boundary is real

**Status:** Accepted; executed 2026-09-24 (preconditions #75–#77 met)

**Context.** The platform (contracts, Go backend, conformance tests, platform Rust components) and the Music product have different release cycles and languages. Splitting before module boundaries are clean would only move coupling across repositories.

**Decision.** A dedicated platform repository will hold contracts, schemas, conformance tests, the Go backend and platform-level Rust components. This repository remains the Music product and the Apple/Swift client implementation (including AppFoundation until a second Apple product needs it elsewhere). The split happens when: AppFoundation has no domain dependencies (#75), Music code lives in domain packages (#76), and kernel contract v0 exists (#77). Until then, platform documents live here.

**Consequences.** Platform docs (`Platform.md`, kernel ADRs) move to the platform repository at split time and this repository keeps a link. Hotel and manufacturing code never land in this repository.

**Execution.** `Contract/` moved here as `contract/` with its history; `Platform.md` and ADR-0001 to ADR-0003 moved to `docs/`. MSRU keeps a pinned copy of the vectors it conforms to (`Packages/MusicDomain/Tests/MusicLibraryTests/Vectors`) and refreshes it when the contract version changes. There is no remote yet; creating one is the owner's decision.

**Revisit when** the preconditions are met, or if keeping platform work here starts to couple it to Swift/Xcode tooling.
