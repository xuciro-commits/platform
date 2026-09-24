import { invoke } from "@tauri-apps/api/core";

// Typed wrappers over the Tauri commands (slices/hotel/client/src-tauri/src/main.rs).

export type Stay = { roomType: string; checkIn: string; checkOut: string };
export type Reservation = Stay & { id: string; guest: string; version: number; canceled: boolean };
export type OutboxEntry = {
  key: string; state: string; schema: string; reservation: string; outcome: string;
  payload: (Partial<Stay> & { guest?: string; expectedVersion?: number }) | null;
};
export type Snapshot = {
  principal: string; tenant: string; online: boolean; outbox: OutboxEntry[]; reservations: Reservation[] | null;
};

export const inTauri = "__TAURI_INTERNALS__" in window;

export const api = {
  login: (server: string, token: string) => invoke<{ principalId: string; tenantId: string; role: string }>("login", { server, token }),
  draft: (schema: "create" | "modify" | "cancel", reservationId: string | null, payload: object) =>
    invoke<void>("draft", { schema, reservationId, payload }),
  revise: (idempotencyKey: string, payload: object) => invoke<void>("revise", { idempotencyKey, payload }),
  send: () => invoke<void>("send"),
  snapshot: () => invoke<Snapshot>("snapshot"),
};
