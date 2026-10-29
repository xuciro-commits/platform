
import type { GenEnum, GenFile, GenMessage } from "@bufbuild/protobuf/codegenv2";
import { enumDesc, fileDesc, messageDesc } from "@bufbuild/protobuf/codegenv2";
import type { Message } from "@bufbuild/protobuf";

export const file_platform_kernel_v1alpha1_work: GenFile =
  fileDesc("CiNwbGF0Zm9ybS9rZXJuZWwvdjFhbHBoYTEvd29yay5wcm90bxIYcGxhdGZvcm0ua2VybmVsLnYxYWxwaGExIpcBCgRXb3JrEg8KB3dvcmtfaWQYASABKAkSEAoIb3duZXJfaWQYAiABKAkSEgoKZ2VuZXJhdGlvbhgDIAEoDRIyCgVzdGF0ZRgEIAEoDjIjLnBsYXRmb3JtLmtlcm5lbC52MWFscGhhMS5Xb3JrU3RhdGUSEAoIcHJvZ3Jlc3MYBSABKA0SEgoKY2hlY2twb2ludBgGIAEoCSqKAQoJV29ya1N0YXRlEhoKFldPUktfU1RBVEVfVU5TUEVDSUZJRUQQABIWChJXT1JLX1NUQVRFX1JVTk5JTkcQARIYChRXT1JLX1NUQVRFX0NPTVBMRVRFRBACEhUKEVdPUktfU1RBVEVfRkFJTEVEEAMSGAoUV09SS19TVEFURV9DQU5DRUxMRUQQBGIGcHJvdG8z");

export type Work = Message<"platform.kernel.v1alpha1.Work"> & {
  workId: string;

  ownerId: string;

  generation: number;

  state: WorkState;

  progress: number;

  checkpoint: string;
};

export type WorkJson = {
  workId?: string;

  ownerId?: string;

  generation?: number;

  state?: WorkStateJson;

  progress?: number;

  checkpoint?: string;
};

export const WorkSchema: GenMessage<Work, {jsonType: WorkJson}> =
  messageDesc(file_platform_kernel_v1alpha1_work, 0);

export enum WorkState {
  UNSPECIFIED = 0,

  RUNNING = 1,

  COMPLETED = 2,

  FAILED = 3,

  CANCELLED = 4,
}

export type WorkStateJson = "WORK_STATE_UNSPECIFIED" | "WORK_STATE_RUNNING" | "WORK_STATE_COMPLETED" | "WORK_STATE_FAILED" | "WORK_STATE_CANCELLED";

export const WorkStateSchema: GenEnum<WorkState, WorkStateJson> =
  enumDesc(file_platform_kernel_v1alpha1_work, 0);

