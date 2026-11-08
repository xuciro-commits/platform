
import type { GenFile, GenMessage } from "@bufbuild/protobuf/codegenv2";
import { fileDesc, messageDesc } from "@bufbuild/protobuf/codegenv2";
import type { Message } from "@bufbuild/protobuf";

export const file_platform_kernel_v1alpha1_tenancy: GenFile =
  fileDesc("CiZwbGF0Zm9ybS9rZXJuZWwvdjFhbHBoYTEvdGVuYW5jeS5wcm90bxIYcGxhdGZvcm0ua2VybmVsLnYxYWxwaGExIjEKBkNhbGxlchIRCgl0ZW5hbnRfaWQYASABKAkSFAoMcHJpbmNpcGFsX2lkGAIgASgJYgZwcm90bzM");

export type Caller = Message<"platform.kernel.v1alpha1.Caller"> & {
  tenantId: string;

  principalId: string;
};

export type CallerJson = {
  tenantId?: string;

  principalId?: string;
};

export const CallerSchema: GenMessage<Caller, {jsonType: CallerJson}> =
  messageDesc(file_platform_kernel_v1alpha1_tenancy, 0);

