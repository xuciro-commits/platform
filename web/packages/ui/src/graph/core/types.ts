import type { ReactNode } from "react";

export type CanvasPosition = { x: number; y: number };

export type CanvasDirection = "right" | "down";

export type CanvasAction = { id: string; label: string; hint?: string; icon?: ReactNode; tone?: "default" | "danger"; disabled?: boolean; run: () => void };

export type CanvasBox = { width: number; height: number };
