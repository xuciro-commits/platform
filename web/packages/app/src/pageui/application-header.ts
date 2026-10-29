import type {Api} from "@platform/kernel";
import {validStaticImageURL} from "@platform/ui";

// Header appearance is checked where it is edited and rendered. Its page
// references still pass the host's application and current-access checks.
export function validApplicationHeader(header: Api.ApplicationHeader | undefined, pages: readonly string[]): boolean {
  if (!header) return true;
  if (!["horizontal", "vertical"].includes(header.variant) || header.title.length > 1024 || !header.items.length || header.items.length > 24 || header.collapsed && header.variant !== "vertical" || header.logo && !validStaticImageURL(header.logo)) return false;
  const seen = new Set<string>();
  return header.items.every(item => {
    if ((item.label?.length ?? 0) > 1024 || (item.text?.length ?? 0) > 1024 || (item.pages?.length ?? 0) > 32) return false;
    switch (item.kind) {
      case "logo": case "title": case "spacer": return !item.pages?.length && !item.label && !item.text && !item.action;
      case "tabs": return !item.label && !item.text && !item.action && !!item.pages?.length && item.pages.every(page => { if (!pages.includes(page) || seen.has(page)) return false; seen.add(page); return true; });
      case "text": return !item.pages?.length && !item.label && !item.action;
      case "button": return !item.pages?.length && !!item.label && !item.text && ["refresh", "theme"].includes(item.action ?? "");
      default: return false;
    }
  });
}
