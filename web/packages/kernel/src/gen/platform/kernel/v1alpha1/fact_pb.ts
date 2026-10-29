
import type { GenEnum, GenFile, GenMessage } from "@bufbuild/protobuf/codegenv2";
import { enumDesc, fileDesc, messageDesc } from "@bufbuild/protobuf/codegenv2";
import type { Timestamp, TimestampJson } from "@bufbuild/protobuf/wkt";
import { file_google_protobuf_timestamp } from "@bufbuild/protobuf/wkt";
import type { SchemaRef, SchemaRefJson } from "./change_pb";
import { file_platform_kernel_v1alpha1_change } from "./change_pb";
import type { EntityRef, EntityRefJson } from "./identity_pb";
import { file_platform_kernel_v1alpha1_identity } from "./identity_pb";
import type { Message } from "@bufbuild/protobuf";

export const file_platform_kernel_v1alpha1_fact: GenFile =
  fileDesc("CiNwbGF0Zm9ybS9rZXJuZWwvdjFhbHBoYTEvZmFjdC5wcm90bxIYcGxhdGZvcm0ua2VybmVsLnYxYWxwaGExIosBCgpQcm92ZW5hbmNlEhYKDHByaW5jaXBhbF9pZBgBIAEoCUgAEhYKDGNvbm5lY3Rvcl9pZBgCIAEoCUgAEi8KC3NvdXJjZV90aW1lGAMgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcBISCgpjb25maWRlbmNlGAQgASgBQggKBnNvdXJjZSLDAgoERmFjdBIRCgl0ZW5hbnRfaWQYASABKAkSMAoEa2luZBgCIAEoDjIiLnBsYXRmb3JtLmtlcm5lbC52MWFscGhhMS5GYWN0S2luZBI0CgdzdWJqZWN0GAMgASgLMiMucGxhdGZvcm0ua2VybmVsLnYxYWxwaGExLkVudGl0eVJlZhIRCglhdHRyaWJ1dGUYBCABKAkSMwoGc2NoZW1hGAUgASgLMiMucGxhdGZvcm0ua2VybmVsLnYxYWxwaGExLlNjaGVtYVJlZhI4Cgpwcm92ZW5hbmNlGAYgASgLMiQucGxhdGZvcm0ua2VybmVsLnYxYWxwaGExLlByb3ZlbmFuY2USFAoMZGVyaXZlZF9mcm9tGAcgAygJEhcKD2lkZW1wb3RlbmN5X2tleRgIIAEoCRIPCgdwYXlsb2FkGAkgASgMIn4KCkZhY3RSZWNvcmQSDwoHZmFjdF9pZBgBIAEoCRIsCgRmYWN0GAIgASgLMh4ucGxhdGZvcm0ua2VybmVsLnYxYWxwaGExLkZhY3QSMQoNcmVjb3JkZWRfdGltZRgDIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXAqbAoIRmFjdEtpbmQSGQoVRkFDVF9LSU5EX1VOU1BFQ0lGSUVEEAASGQoVRkFDVF9LSU5EX09CU0VSVkFUSU9OEAESEwoPRkFDVF9LSU5EX0NMQUlNEAISFQoRRkFDVF9LSU5EX0RFUklWRUQQA2IGcHJvdG8z", [file_google_protobuf_timestamp, file_platform_kernel_v1alpha1_change, file_platform_kernel_v1alpha1_identity]);

export type Provenance = Message<"platform.kernel.v1alpha1.Provenance"> & {
  source: {
    value: string;
    case: "principalId";
  } | {
    value: string;
    case: "connectorId";
  } | { case: undefined; value?: undefined };

  sourceTime?: Timestamp | undefined;

  confidence: number;
};

export type ProvenanceJson = {
  principalId?: string;

  connectorId?: string;

  sourceTime?: TimestampJson;

  confidence?: number | "NaN" | "Infinity" | "-Infinity";
};

export const ProvenanceSchema: GenMessage<Provenance, {jsonType: ProvenanceJson}> =
  messageDesc(file_platform_kernel_v1alpha1_fact, 0);

export type Fact = Message<"platform.kernel.v1alpha1.Fact"> & {
  tenantId: string;

  kind: FactKind;

  subject?: EntityRef | undefined;

  attribute: string;

  schema?: SchemaRef | undefined;

  provenance?: Provenance | undefined;

  derivedFrom: string[];

  idempotencyKey: string;

  payload: Uint8Array;
};

export type FactJson = {
  tenantId?: string;

  kind?: FactKindJson;

  subject?: EntityRefJson;

  attribute?: string;

  schema?: SchemaRefJson;

  provenance?: ProvenanceJson;

  derivedFrom?: string[];

  idempotencyKey?: string;

  payload?: string;
};

export const FactSchema: GenMessage<Fact, {jsonType: FactJson}> =
  messageDesc(file_platform_kernel_v1alpha1_fact, 1);

export type FactRecord = Message<"platform.kernel.v1alpha1.FactRecord"> & {
  factId: string;

  fact?: Fact | undefined;

  recordedTime?: Timestamp | undefined;
};

export type FactRecordJson = {
  factId?: string;

  fact?: FactJson;

  recordedTime?: TimestampJson;
};

export const FactRecordSchema: GenMessage<FactRecord, {jsonType: FactRecordJson}> =
  messageDesc(file_platform_kernel_v1alpha1_fact, 2);

export enum FactKind {
  UNSPECIFIED = 0,

  OBSERVATION = 1,

  CLAIM = 2,

  DERIVED = 3,
}

export type FactKindJson = "FACT_KIND_UNSPECIFIED" | "FACT_KIND_OBSERVATION" | "FACT_KIND_CLAIM" | "FACT_KIND_DERIVED";

export const FactKindSchema: GenEnum<FactKind, FactKindJson> =
  enumDesc(file_platform_kernel_v1alpha1_fact, 0);

