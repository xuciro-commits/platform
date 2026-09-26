# ADR-0002: The kernel is a language-neutral contract

**Status:** Accepted (2026-09-24); format decided 2026-09-24. Amended by ADR-0025 D5: the Swift implementation is deleted; Go, Rust and TypeScript implement the contract.

**Context.** Go (server), Rust (systems components, Tauri desktop) and Swift (Apple edge) will coexist. A kernel defined as one language's library would make that language the platform and let semantics drift in the others.

**Decision.** The kernel is its schemas, semantic rules and conformance test vectors. Go is the reference implementation. Other runtimes implement the contract and pass the same vectors, or map to it at their boundary. Cross-language boundaries are introduced only where justified; no default "implement everything in every language". Whether Swift and Tauri share a Rust edge core: not yet (ADR-0005).

**Format.** Protobuf describes the data contract only; semantics are numbered rules in prose with an error per violation; conformance is language-neutral JSON vectors run unchanged by every implementation; errors are one shared code set. Protobuf was chosen for field-number evolution rules, `buf breaking` enforcement and generators for Go, Rust, Swift and TypeScript; it must not become the definition of kernel semantics. The contract lives in `contract/` until the platform repository split (ADR-0003). Details: `docs/Platform.md`, Kernel Contract.

**Consequences.** Kernel changes are contract changes: they update schemas and vectors first, and every implementation must pass. The server is not the kernel.

**Revisit when** maintaining conformance across runtimes costs more than a shared implementation would.
