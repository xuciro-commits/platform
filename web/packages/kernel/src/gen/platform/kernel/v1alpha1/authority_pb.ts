
import type { GenEnum, GenFile, GenMessage } from "@bufbuild/protobuf/codegenv2";
import { enumDesc, fileDesc, messageDesc } from "@bufbuild/protobuf/codegenv2";
import type { Message } from "@bufbuild/protobuf";

export const file_platform_kernel_v1alpha1_authority: GenFile =
  fileDesc("CihwbGF0Zm9ybS9rZXJuZWwvdjFhbHBoYTEvYXV0aG9yaXR5LnByb3RvEhhwbGF0Zm9ybS5rZXJuZWwudjFhbHBoYTEimQEKFEF1dGhvcml0eURlY2xhcmF0aW9uEhEKCXRlbmFudF9pZBgBIAEoCRISCgpkYXRhX2NsYXNzGAIgASgJEjUKBGtpbmQYAyABKA4yJy5wbGF0Zm9ybS5rZXJuZWwudjFhbHBoYTEuQXV0aG9yaXR5S2luZBIUCgxhdXRob3JpdHlfaWQYBCABKAkSDQoFZXBvY2gYBSABKA0qiQEKDUF1dGhvcml0eUtpbmQSHgoaQVVUSE9SSVRZX0tJTkRfVU5TUEVDSUZJRUQQABIZChVBVVRIT1JJVFlfS0lORF9ERVZJQ0UQARIgChxBVVRIT1JJVFlfS0lORF9URU5BTlRfU0VSVkVSEAISGwoXQVVUSE9SSVRZX0tJTkRfRVhURVJOQUwQAyrrAQoPU3VibWlzc2lvblN0YXRlEiAKHFNVQk1JU1NJT05fU1RBVEVfVU5TUEVDSUZJRUQQABIcChhTVUJNSVNTSU9OX1NUQVRFX1BFTkRJTkcQARIcChhTVUJNSVNTSU9OX1NUQVRFX1NFTkRJTkcQAhIeChpTVUJNSVNTSU9OX1NUQVRFX0NPTkZJUk1FRBADEh0KGVNVQk1JU1NJT05fU1RBVEVfQ09ORkxJQ1QQBBIdChlTVUJNSVNTSU9OX1NUQVRFX1JFSkVDVEVEEAUSHAoYU1VCTUlTU0lPTl9TVEFURV9VTktOT1dOEAZiBnByb3RvMw");

export type AuthorityDeclaration = Message<"platform.kernel.v1alpha1.AuthorityDeclaration"> & {
  tenantId: string;

  dataClass: string;

  kind: AuthorityKind;

  authorityId: string;

  epoch: number;
};

export type AuthorityDeclarationJson = {
  tenantId?: string;

  dataClass?: string;

  kind?: AuthorityKindJson;

  authorityId?: string;

  epoch?: number;
};

export const AuthorityDeclarationSchema: GenMessage<AuthorityDeclaration, {jsonType: AuthorityDeclarationJson}> =
  messageDesc(file_platform_kernel_v1alpha1_authority, 0);

export enum AuthorityKind {
  UNSPECIFIED = 0,

  DEVICE = 1,

  TENANT_SERVER = 2,

  EXTERNAL = 3,
}

export type AuthorityKindJson = "AUTHORITY_KIND_UNSPECIFIED" | "AUTHORITY_KIND_DEVICE" | "AUTHORITY_KIND_TENANT_SERVER" | "AUTHORITY_KIND_EXTERNAL";

export const AuthorityKindSchema: GenEnum<AuthorityKind, AuthorityKindJson> =
  enumDesc(file_platform_kernel_v1alpha1_authority, 0);

export enum SubmissionState {
  UNSPECIFIED = 0,

  PENDING = 1,

  SENDING = 2,

  CONFIRMED = 3,

  CONFLICT = 4,

  REJECTED = 5,

  UNKNOWN = 6,
}

export type SubmissionStateJson = "SUBMISSION_STATE_UNSPECIFIED" | "SUBMISSION_STATE_PENDING" | "SUBMISSION_STATE_SENDING" | "SUBMISSION_STATE_CONFIRMED" | "SUBMISSION_STATE_CONFLICT" | "SUBMISSION_STATE_REJECTED" | "SUBMISSION_STATE_UNKNOWN";

export const SubmissionStateSchema: GenEnum<SubmissionState, SubmissionStateJson> =
  enumDesc(file_platform_kernel_v1alpha1_authority, 1);

