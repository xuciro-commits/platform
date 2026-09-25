// Shapes of mes-server's read API (slices/manufacturing/server).
export type Operation = { step: number; name: string; workCenter: string };
export type Product = { id: string; name: string; routing: string; operations: Operation[] };
export type WorkCenter = { id: string; name: string; line: string; resources: string[] };
export type Master = { products: Product[]; workCenters: WorkCenter[] };
export type Order = { id: string; product: string; quantity: number; sfcs: string[]; planned?: string;
  erp?: "sent" | "confirmed" | "refused" | "failed"; confirmation?: string; erpDetail?: string; resent?: number };
export type SFC = {
  id: string; order: string; product: string; step: number; state: "queued" | "active" | "hold" | "done" | "scrapped";
  resource?: string; revision: number; ncs: { step: number; code: string; by: string }[]; signatures: { action: string; meaning: string; by: string }[];
};
export type Planned = { erpId: string; product: string; quantity: number; due: string; factId: string };
export type Downtime = { id: string; resource: string; start: string; end?: string; reason?: string; needsCheck?: boolean };

export const ncCodes = ["POROSITY", "DIMENSION", "SURFACE", "LEAK"];
export const downtimeReasons = ["Tool change", "Setup", "Material shortage", "Breakdown", "Quality issue"];
