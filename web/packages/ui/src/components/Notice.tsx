import { Banner, type BannerStatus } from "@astryxdesign/core/Banner";
import type { Tone } from "./StatusTag";

export function Notice({
  title,
  label,
  message,
  tone,
  role,
}: {
  title?: string;
  label?: string;
  role?: "note" | "status" | "alert";
  message: string;
  tone: Exclude<Tone, "neutral">;
}) {
  const status: BannerStatus =
    tone === "success" ? "success" : tone === "warning" ? "warning" : tone === "danger" ? "error" : "info";
  return (
    <Banner
      status={status}
      title={title ?? message}
      description={title ? message : undefined}
      role={role ?? (tone === "info" || tone === "success" ? "status" : "alert")}
      aria-label={label ?? title}
      container="card"
    />
  );
}
