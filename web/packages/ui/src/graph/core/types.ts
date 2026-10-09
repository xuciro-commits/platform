import type { ReactNode } from "react";

// Vocabulary both canvas families share. A family owns meaning (what a node is,
// what connecting two of them says); the core owns the drawing surface: the
// frame, the viewport, the toolbar, the help, the action bar and the layouts.

/** One point on the canvas. The owner persists it; the canvas never invents it. */
export type CanvasPosition = { x: number; y: number };

/** Which way a layered drawing runs. */
export type CanvasDirection = "right" | "down";

/** One thing a person may do to the selected element; the owner supplies the
 * meaning and the run, the canvas supplies the place and the wording (ADR-0084 D3). */
export type CanvasAction = { id: string; label: string; hint?: string; icon?: ReactNode; tone?: "default" | "danger"; disabled?: boolean; run: () => void };

/** A box a layout measures; both families lay out boxes, not pixels. */
export type CanvasBox = { width: number; height: number };
