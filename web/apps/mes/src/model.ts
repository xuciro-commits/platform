// Shapes of mes-server's read API (slices/manufacturing/server).
export type Operation = { step: number; name: string; workCenter: string };
export type Product = { id: string; name: string; routing: string; operations: Operation[] };
export type WorkCenter = { id: string; name: string; line: string; resources: string[] };
export type Master = { products: Product[]; workCenters: WorkCenter[] };
export type Order = { id: string; product: string; quantity: number; sfcs: string[]; planned?: string;
  erp?: "sent" | "confirmed" | "refused" | "failed"; confirmation?: string; erpDetail?: string };
export type SFC = {
  id: string; order: string; product: string; step: number; state: "queued" | "active" | "hold" | "done" | "scrapped";
  resource?: string; revision: number; ncs: { step: number; code: string; by: string }[]; signatures: { action: string; meaning: string; by: string }[];
};
export type Planned = { erpId: string; product: string; quantity: number; due: string; factId: string };
export type Downtime = { id: string; resource: string; start: string; end?: string; reason?: string; needsCheck?: boolean };
export type Notification = { id: string; app: string; title: string; body?: string; ref?: string; at: string; read: boolean };
/** The signed-in member (ADR-0010): one role per app, attributes such as lines. */
export type Me = { tenantId: string; principalId: string; profile: { roles: Record<string, string>; attributes?: { lines?: string[] } } };

export const SERVER = (import.meta.env.VITE_MES_SERVER as string | undefined) ?? "http://127.0.0.1:8490";
export const identities = [
  { id: "supervisor", label: "Supervisor · lines L1, L2" },
  { id: "operator-l1", label: "Operator · line L1" },
  { id: "operator-l2", label: "Operator · line L2" },
  { id: "quality-1", label: "Quality engineer 1" },
  { id: "quality-2", label: "Quality engineer 2" },
];
export const ncCodes = ["POROSITY", "DIMENSION", "SURFACE", "LEAK"];
export const downtimeReasons = ["Tool change", "Setup", "Material shortage", "Breakdown", "Quality issue"];
