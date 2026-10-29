
import type { GenFile, GenMessage } from "@bufbuild/protobuf/codegenv2";
import { fileDesc, messageDesc } from "@bufbuild/protobuf/codegenv2";
import type { Timestamp, TimestampJson } from "@bufbuild/protobuf/wkt";
import { file_google_protobuf_timestamp } from "@bufbuild/protobuf/wkt";
import type { EntityRef, EntityRefJson } from "./identity_pb";
import { file_platform_kernel_v1alpha1_identity } from "./identity_pb";
import type { Message } from "@bufbuild/protobuf";

export const file_platform_kernel_v1alpha1_change: GenFile =
  fileDesc("CiVwbGF0Zm9ybS9rZXJuZWwvdjFhbHBoYTEvY2hhbmdlLnByb3RvEhhwbGF0Zm9ybS5rZXJuZWwudjFhbHBoYTEiKgoJU2NoZW1hUmVmEgwKBG5hbWUYASABKAkSDwoHdmVyc2lvbhgCIAEoDSKLAwoKU3VibWlzc2lvbhIRCgl0ZW5hbnRfaWQYASABKAkSFAoMcHJpbmNpcGFsX2lkGAIgASgJEhEKCWF1dGhvcml0eRgDIAEoCRIzCgZ0YXJnZXQYBCABKAsyIy5wbGF0Zm9ybS5rZXJuZWwudjFhbHBoYTEuRW50aXR5UmVmEjMKBnNjaGVtYRgFIAEoCzIjLnBsYXRmb3JtLmtlcm5lbC52MWFscGhhMS5TY2hlbWFSZWYSLgoKdmFsaWRfdGltZRgGIAEoCzIaLmdvb2dsZS5wcm90b2J1Zi5UaW1lc3RhbXASFAoMY2F1c2F0aW9uX2lkGAcgASgJEhYKDmNvcnJlbGF0aW9uX2lkGAggASgJEhcKD2lkZW1wb3RlbmN5X2tleRgJIAEoCRIPCgdwYXlsb2FkGAogASgMEhkKEWV2aWRlbmNlX2ZhY3RfaWRzGAsgAygJEh4KEWV4cGVjdGVkX3JldmlzaW9uGAwgASgNSACIAQFCFAoSX2V4cGVjdGVkX3JldmlzaW9uItABCgxDaGFuZ2VSZWNvcmQSEQoJY2hhbmdlX2lkGAEgASgJEjgKCnN1Ym1pc3Npb24YAiABKAsyJC5wbGF0Zm9ybS5rZXJuZWwudjFhbHBoYTEuU3VibWlzc2lvbhIuCgp2YWxpZF90aW1lGAMgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcBIxCg1yZWNvcmRlZF90aW1lGAQgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcBIQCghyZXZpc2lvbhgFIAEoDWIGcHJvdG8z", [file_google_protobuf_timestamp, file_platform_kernel_v1alpha1_identity]);

export type SchemaRef = Message<"platform.kernel.v1alpha1.SchemaRef"> & {
  name: string;

  version: number;
};

export type SchemaRefJson = {
  name?: string;

  version?: number;
};

export const SchemaRefSchema: GenMessage<SchemaRef, {jsonType: SchemaRefJson}> =
  messageDesc(file_platform_kernel_v1alpha1_change, 0);

export type Submission = Message<"platform.kernel.v1alpha1.Submission"> & {
  tenantId: string;

  principalId: string;

  authority: string;

  target?: EntityRef | undefined;

  schema?: SchemaRef | undefined;

  validTime?: Timestamp | undefined;

  causationId: string;

  correlationId: string;

  idempotencyKey: string;

  payload: Uint8Array;

  evidenceFactIds: string[];

  expectedRevision?: number | undefined;
};

export type SubmissionJson = {
  tenantId?: string;

  principalId?: string;

  authority?: string;

  target?: EntityRefJson;

  schema?: SchemaRefJson;

  validTime?: TimestampJson;

  causationId?: string;

  correlationId?: string;

  idempotencyKey?: string;

  payload?: string;

  evidenceFactIds?: string[];

  expectedRevision?: number;
};

export const SubmissionSchema: GenMessage<Submission, {jsonType: SubmissionJson}> =
  messageDesc(file_platform_kernel_v1alpha1_change, 1);

export type ChangeRecord = Message<"platform.kernel.v1alpha1.ChangeRecord"> & {
  changeId: string;

  submission?: Submission | undefined;

  validTime?: Timestamp | undefined;

  recordedTime?: Timestamp | undefined;

  revision: number;
};

export type ChangeRecordJson = {
  changeId?: string;

  submission?: SubmissionJson;

  validTime?: TimestampJson;

  recordedTime?: TimestampJson;

  revision?: number;
};

export const ChangeRecordSchema: GenMessage<ChangeRecord, {jsonType: ChangeRecordJson}> =
  messageDesc(file_platform_kernel_v1alpha1_change, 2);

