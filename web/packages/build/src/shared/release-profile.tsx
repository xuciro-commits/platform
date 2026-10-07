// The tenant's release profile (ADR-0048 D5b). A production tenant delivers
// definitions through a saved joint candidate, so its authoring surfaces no
// longer offer the direct install; the development and import profiles keep the
// same implementation, and the owner refuses a bypass either way.
//
// The builder reads it from its own app, not from the platform's settings read:
// a builder without a platform role must still know which entry it may offer.
import { useRead } from "@platform/app";

/** Whether this tenant still offers the direct install. */
export function useDirectInstall(): boolean {
  const profile = useRead<{ profile: string; directInstall: boolean }>("/v1/release-profile");
  return profile ? profile.directInstall : true;
}
