
import type { GenFile, GenMessage } from "@bufbuild/protobuf/codegenv2";
import { fileDesc, messageDesc } from "@bufbuild/protobuf/codegenv2";
import type { SchemaRef, SchemaRefJson } from "./change_pb";
import { file_platform_kernel_v1alpha1_change } from "./change_pb";
import type { Message } from "@bufbuild/protobuf";

export const file_platform_kernel_v1alpha1_schema: GenFile =
  fileDesc("CiVwbGF0Zm9ybS9rZXJuZWwvdjFhbHBoYTEvc2NoZW1hLnByb3RvEhhwbGF0Zm9ybS5rZXJuZWwudjFhbHBoYTEiMQoLVXBncmFkZVN0ZXASDAoEbmFtZRgBIAEoCRIUCgxmcm9tX3ZlcnNpb24YAiABKA0ifQoOU2NoZW1hUmVnaXN0cnkSMgoFa25vd24YASADKAsyIy5wbGF0Zm9ybS5rZXJuZWwudjFhbHBoYTEuU2NoZW1hUmVmEjcKCHVwZ3JhZGVzGAIgAygLMiUucGxhdGZvcm0ua2VybmVsLnYxYWxwaGExLlVwZ3JhZGVTdGVwYgZwcm90bzM", [file_platform_kernel_v1alpha1_change]);

export type UpgradeStep = Message<"platform.kernel.v1alpha1.UpgradeStep"> & {
  name: string;

  fromVersion: number;
};

export type UpgradeStepJson = {
  name?: string;

  fromVersion?: number;
};

export const UpgradeStepSchema: GenMessage<UpgradeStep, {jsonType: UpgradeStepJson}> =
  messageDesc(file_platform_kernel_v1alpha1_schema, 0);

export type SchemaRegistry = Message<"platform.kernel.v1alpha1.SchemaRegistry"> & {
  known: SchemaRef[];

  upgrades: UpgradeStep[];
};

export type SchemaRegistryJson = {
  known?: SchemaRefJson[];

  upgrades?: UpgradeStepJson[];
};

export const SchemaRegistrySchema: GenMessage<SchemaRegistry, {jsonType: SchemaRegistryJson}> =
  messageDesc(file_platform_kernel_v1alpha1_schema, 1);

