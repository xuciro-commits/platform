# ADR-0005: No shared Rust edge core yet

**Status:** Accepted (2026-09-24)

**Context.** ADR-0002 deferred whether Swift (Music) and Tauri clients share a Rust edge core. Both edges now exist: MSRU's SQLite decision log (K4) and the Hotel desk's Rust outbox (K5), each passing the contract's vectors. A shared core would bring FFI bindings into the Apple build and a second toolchain into every edge release.

**Decision.** Each edge implements the contract natively and proves it with the shared vectors. The edge logic so far is small (an outbox state machine, an append-only log with idempotency: a few hundred lines per language), and the vectors keep the implementations equal. No Rust crate is linked into Swift.

**Consequences.** A rule change costs one change per edge language, caught by the vectors. The Rust outbox lives inside the Hotel client until a second Rust client needs it.

**Revisit when** edge logic grows beyond state machines — local storage with sync cursors, conflict merging for shared libraries (drill E2), offline caches of server data — or when a third edge language appears.
