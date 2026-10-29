export function resolvePageLiveMode(explicit: boolean | undefined, holderPreview: boolean, previewRoute: boolean): boolean {
  if (explicit === false) return false;
  return !holderPreview && !previewRoute;
}
