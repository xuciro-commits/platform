import { invoke } from "@tauri-apps/api/core";

// Typed wrappers over the Tauri commands (slices/hotel/client/src-tauri/src/main.rs).

import type { Reservation, Stay } from "@pkg/hotel";
export type OutboxEntry = {
  key: string; state: string; schema: string; reservation: string; outcome: string;
  payload: (Partial<Stay> & { guest?: string }) | null;
};
export type Snapshot = {
  principal: string; tenant: string; online: boolean; outbox: OutboxEntry[]; reservations: Reservation[] | null;
};

export const inTauri = "__TAURI_INTERNALS__" in window;

export const api = {
  login: (server: string, token: string) => invoke<{ principalId: string; tenantId: string; role: string }>("login", { server, token }),
  /** `expectedRevision`: the reservation's revision this decision was made on (K4 C12); 0 for a new one. */
  draft: (schema: "create" | "modify" | "cancel", reservationId: string | null, payload: object, expectedRevision: number) =>
    invoke<void>("draft", { schema, reservationId, payload, expectedRevision }),
  revise: (idempotencyKey: string, payload: object) => invoke<void>("revise", { idempotencyKey, payload }),
  send: () => invoke<void>("send"),
  snapshot: () => invoke<Snapshot>("snapshot"),
};
