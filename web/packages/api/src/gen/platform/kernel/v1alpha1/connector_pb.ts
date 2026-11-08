
import type { GenEnum, GenFile, GenMessage } from "@bufbuild/protobuf/codegenv2";
import { enumDesc, fileDesc, messageDesc } from "@bufbuild/protobuf/codegenv2";
import type { Duration, DurationJson, Timestamp, TimestampJson } from "@bufbuild/protobuf/wkt";
import { file_google_protobuf_duration, file_google_protobuf_timestamp } from "@bufbuild/protobuf/wkt";
import type { Message } from "@bufbuild/protobuf";

export const file_platform_kernel_v1alpha1_connector: GenFile =
  fileDesc("CihwbGF0Zm9ybS9rZXJuZWwvdjFhbHBoYTEvY29ubmVjdG9yLnByb3RvEhhwbGF0Zm9ybS5rZXJuZWwudjFhbHBoYTEi1QEKE0Nvbm5lY3RvckRlc2NyaXB0b3ISEQoJdGVuYW50X2lkGAEgASgJEhQKDGNvbm5lY3Rvcl9pZBgCIAEoCRI/CglkaXJlY3Rpb24YAyABKA4yLC5wbGF0Zm9ybS5rZXJuZWwudjFhbHBoYTEuQ29ubmVjdG9yRGlyZWN0aW9uEhQKDGRhdGFfY2xhc3NlcxgEIAMoCRIsCgloZWFydGJlYXQYBSABKAsyGS5nb29nbGUucHJvdG9idWYuRHVyYXRpb24SEAoIZGlzYWJsZWQYBiABKAgioQEKD0Nvbm5lY3RvclN0YXR1cxIUCgxjb25uZWN0b3JfaWQYASABKAkSOQoGaGVhbHRoGAIgASgOMikucGxhdGZvcm0ua2VybmVsLnYxYWxwaGExLkNvbm5lY3RvckhlYWx0aBItCglsYXN0X3NlZW4YAyABKAsyGi5nb29nbGUucHJvdG9idWYuVGltZXN0YW1wEg4KBmN1cnNvchgEIAEoCSp1ChJDb25uZWN0b3JEaXJlY3Rpb24SIwofQ09OTkVDVE9SX0RJUkVDVElPTl9VTlNQRUNJRklFRBAAEhwKGENPTk5FQ1RPUl9ESVJFQ1RJT05fUFVTSBABEhwKGENPTk5FQ1RPUl9ESVJFQ1RJT05fUE9MTBACKocBCg9Db25uZWN0b3JIZWFsdGgSIAocQ09OTkVDVE9SX0hFQUxUSF9VTlNQRUNJRklFRBAAEhcKE0NPTk5FQ1RPUl9IRUFMVEhfT0sQARIaChZDT05ORUNUT1JfSEVBTFRIX1NUQUxFEAISHQoZQ09OTkVDVE9SX0hFQUxUSF9ESVNBQkxFRBADYgZwcm90bzM", [file_google_protobuf_duration, file_google_protobuf_timestamp]);

export type ConnectorDescriptor = Message<"platform.kernel.v1alpha1.ConnectorDescriptor"> & {
  tenantId: string;

  connectorId: string;

  direction: ConnectorDirection;

  dataClasses: string[];

  heartbeat?: Duration | undefined;

  disabled: boolean;
};

export type ConnectorDescriptorJson = {
  tenantId?: string;

  connectorId?: string;

  direction?: ConnectorDirectionJson;

  dataClasses?: string[];

  heartbeat?: DurationJson;

  disabled?: boolean;
};

export const ConnectorDescriptorSchema: GenMessage<ConnectorDescriptor, {jsonType: ConnectorDescriptorJson}> =
  messageDesc(file_platform_kernel_v1alpha1_connector, 0);

export type ConnectorStatus = Message<"platform.kernel.v1alpha1.ConnectorStatus"> & {
  connectorId: string;

  health: ConnectorHealth;

  lastSeen?: Timestamp | undefined;

  cursor: string;
};

export type ConnectorStatusJson = {
  connectorId?: string;

  health?: ConnectorHealthJson;

  lastSeen?: TimestampJson;

  cursor?: string;
};

export const ConnectorStatusSchema: GenMessage<ConnectorStatus, {jsonType: ConnectorStatusJson}> =
  messageDesc(file_platform_kernel_v1alpha1_connector, 1);

export enum ConnectorDirection {
  UNSPECIFIED = 0,

  PUSH = 1,

  POLL = 2,
}

export type ConnectorDirectionJson = "CONNECTOR_DIRECTION_UNSPECIFIED" | "CONNECTOR_DIRECTION_PUSH" | "CONNECTOR_DIRECTION_POLL";

export const ConnectorDirectionSchema: GenEnum<ConnectorDirection, ConnectorDirectionJson> =
  enumDesc(file_platform_kernel_v1alpha1_connector, 0);

export enum ConnectorHealth {
  UNSPECIFIED = 0,

  OK = 1,

  STALE = 2,

  DISABLED = 3,
}

export type ConnectorHealthJson = "CONNECTOR_HEALTH_UNSPECIFIED" | "CONNECTOR_HEALTH_OK" | "CONNECTOR_HEALTH_STALE" | "CONNECTOR_HEALTH_DISABLED";

export const ConnectorHealthSchema: GenEnum<ConnectorHealth, ConnectorHealthJson> =
  enumDesc(file_platform_kernel_v1alpha1_connector, 1);

