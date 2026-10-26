/** Any enclosing authoring surface restricts a page to preview. An explicit
 * live prop or a published application cannot lift that restriction. */
export function resolvePageLiveMode(explicit: boolean | undefined, holderPreview: boolean, previewRoute: boolean): boolean {
  if (explicit === false) return false;
  return !holderPreview && !previewRoute;
}
