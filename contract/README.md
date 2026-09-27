# Kernel Contract (v1alpha1)

Language-neutral contract for business systems (ADR-0002). The kernel defines schema, semantics, errors, compatibility, conformance, and scope as separate parts. No domain vocabulary is allowed in this module.

## Layout

- `proto/`: Protobuf schemas (`platform.kernel.v1alpha1`), validated by `buf lint`.
- `spec/`: Semantic rules with error codes ([contract/spec/README.md](spec/README.md)).
- `vectors/`: Language-neutral conformance vectors in JSON.
- `go/`: Go reference implementation (`platform/kernel`) verifying all vectors.
