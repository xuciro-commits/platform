# ADR-0001: The platform is a business platform; AppFoundation is an Apple client layer

**Status:** Accepted (2026-09-24)

**Context.** The repository grew from a music app plus `AppFoundation`, which was treated as "the platform". Review showed AppFoundation is an Apple UI toolkit (shell, feature host, dependencies, routing, restoration) that had absorbed music types and re-exported music packages and GRDB, while the ideas that are actually foundational (identity/redirects, claims with provenance, source capabilities, review before commit) lived inside the music code. The owner's goal is a multi-tenant business platform with server and edge runtimes that survives domain change.

**Decision.** The platform is defined in `docs/Platform.md` with a working five-layer model (kernel, capabilities, domain models, workflows/policies, runtime configuration) judged by its change gradient. AppFoundation is a domain-neutral Apple client capability; music code lives in the Music domain. Applications (Music, Hotel, manufacturing) validate the platform through cross-domain comparison and evolution drills; they are not its source of truth. The former Stage 3 plan (DocStudio/HotelDesk as AppFoundation UI demos) is withdrawn.

**Consequences.** AppFoundation may not depend on domain packages or storage engines, may not re-export modules, and may not expose domain vocabulary (enforced by `Scripts/verify-architecture.py` in the MSRU repository). Kernel concepts enter only through the promotion rules in Platform.md §4.

**Revisit when** evolution drills show a layer boundary is wrong, or a second Apple product needs a different client layer shape.
