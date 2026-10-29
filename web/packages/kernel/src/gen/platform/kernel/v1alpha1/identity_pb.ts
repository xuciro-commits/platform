
import type { GenEnum, GenFile, GenMessage } from "@bufbuild/protobuf/codegenv2";
import { enumDesc, fileDesc, messageDesc } from "@bufbuild/protobuf/codegenv2";
import type { Message } from "@bufbuild/protobuf";

export const file_platform_kernel_v1alpha1_identity: GenFile =
  fileDesc("CidwbGF0Zm9ybS9rZXJuZWwvdjFhbHBoYTEvaWRlbnRpdHkucHJvdG8SGHBsYXRmb3JtLmtlcm5lbC52MWFscGhhMSIlCglFbnRpdHlSZWYSDAoEdHlwZRgBIAEoCRIKCgJpZBgCIAEoCSKkAQoIUmVkaXJlY3QSMQoEZnJvbRgBIAEoCzIjLnBsYXRmb3JtLmtlcm5lbC52MWFscGhhMS5FbnRpdHlSZWYSLwoCdG8YAiADKAsyIy5wbGF0Zm9ybS5rZXJuZWwudjFhbHBoYTEuRW50aXR5UmVmEjQKBGtpbmQYAyABKA4yJi5wbGF0Zm9ybS5rZXJuZWwudjFhbHBoYTEuUmVkaXJlY3RLaW5kIj8KCkNhbmRpZGF0ZXMSMQoEcmVmcxgBIAMoCzIjLnBsYXRmb3JtLmtlcm5lbC52MWFscGhhMS5FbnRpdHlSZWYiiwEKClJlc29sdXRpb24SNwoIcmVzb2x2ZWQYASABKAsyIy5wbGF0Zm9ybS5rZXJuZWwudjFhbHBoYTEuRW50aXR5UmVmSAASOQoJYW1iaWd1b3VzGAIgASgLMiQucGxhdGZvcm0ua2VybmVsLnYxYWxwaGExLkNhbmRpZGF0ZXNIAEIJCgdvdXRjb21lKl8KDFJlZGlyZWN0S2luZBIdChlSRURJUkVDVF9LSU5EX1VOU1BFQ0lGSUVEEAASFwoTUkVESVJFQ1RfS0lORF9NRVJHRRABEhcKE1JFRElSRUNUX0tJTkRfU1BMSVQQAmIGcHJvdG8z");

export type EntityRef = Message<"platform.kernel.v1alpha1.EntityRef"> & {
  type: string;

  id: string;
};

export type EntityRefJson = {
  type?: string;

  id?: string;
};

export const EntityRefSchema: GenMessage<EntityRef, {jsonType: EntityRefJson}> =
  messageDesc(file_platform_kernel_v1alpha1_identity, 0);

export type Redirect = Message<"platform.kernel.v1alpha1.Redirect"> & {
  from?: EntityRef | undefined;

  to: EntityRef[];

  kind: RedirectKind;
};

export type RedirectJson = {
  from?: EntityRefJson;

  to?: EntityRefJson[];

  kind?: RedirectKindJson;
};

export const RedirectSchema: GenMessage<Redirect, {jsonType: RedirectJson}> =
  messageDesc(file_platform_kernel_v1alpha1_identity, 1);

export type Candidates = Message<"platform.kernel.v1alpha1.Candidates"> & {
  refs: EntityRef[];
};

export type CandidatesJson = {
  refs?: EntityRefJson[];
};

export const CandidatesSchema: GenMessage<Candidates, {jsonType: CandidatesJson}> =
  messageDesc(file_platform_kernel_v1alpha1_identity, 2);

export type Resolution = Message<"platform.kernel.v1alpha1.Resolution"> & {
  outcome: {
    value: EntityRef;
    case: "resolved";
  } | {
    value: Candidates;
    case: "ambiguous";
  } | { case: undefined; value?: undefined };
};

export type ResolutionJson = {
  resolved?: EntityRefJson;

  ambiguous?: CandidatesJson;
};

export const ResolutionSchema: GenMessage<Resolution, {jsonType: ResolutionJson}> =
  messageDesc(file_platform_kernel_v1alpha1_identity, 3);

export enum RedirectKind {
  UNSPECIFIED = 0,

  MERGE = 1,

  SPLIT = 2,
}

export type RedirectKindJson = "REDIRECT_KIND_UNSPECIFIED" | "REDIRECT_KIND_MERGE" | "REDIRECT_KIND_SPLIT";

export const RedirectKindSchema: GenEnum<RedirectKind, RedirectKindJson> =
  enumDesc(file_platform_kernel_v1alpha1_identity, 0);

