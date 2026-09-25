export { Authorities, KernelError, type Entry, type Event, type State } from "./outbox";
export { EdgeClient, type ActionDeclaration, type Connection } from "./client";
export type { SubmissionJson } from "./gen/platform/kernel/v1alpha1/change_pb";
export type { AuthorityDeclarationJson, SubmissionStateJson } from "./gen/platform/kernel/v1alpha1/authority_pb";
export type { ErrorCodeJson } from "./gen/platform/kernel/v1alpha1/error_pb";
export { codeChallenge, currentSession, keepFresh, signIn, signOut, type OidcConfig, type OidcSession } from "./oidc";
// The host API contract (ADR-0023 D7): types generated from the host's Go types.
export type * as Api from "./gen/host";
