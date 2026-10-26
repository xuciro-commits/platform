# Unified Architecture Framework (UAF) 1.3

- Source: https://www.omg.org/spec/UAF/1.3 (downloaded 2026-10-06)
- `uaf-1.3-specification.html`: the specification document (renamed from `1.3.html`).

The machine-readable files live beside their generated runtime data under the enterprise module, which owns standard parsing and generation:

- `capabilities/server/internal/enterprise/uaf/spec/uaf-1.3-profile.xmi` — OMG document dtc/24-11-06, the UAF Profile (272 stereotypes).
- `capabilities/server/internal/enterprise/uaf/spec/uaf-1.3-measurements.xmi` — OMG document dtc/24-11-07, the Measurements Library.

A new UAF release is a new pair of files beside these plus a migration table; the files themselves are never edited.

Run `make generate-uaf` after adding a release. `make check-uaf` compares the
generated runtime metamodel and both source digests without changing files.
