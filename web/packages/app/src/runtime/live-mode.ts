/** Resolves the execution mode of a composed page from every authority that
 * can declare it, in priority order: an explicit prop from an authoring
 * surface, the preview route context, and finally the application holder's
 * published definition. No single authority - in particular not the optional
 * application holder - may own this decision alone. */
export function resolvePageLiveMode(explicit: boolean | undefined, holderPreview: boolean, previewRoute: boolean): boolean {
  if (explicit === false) return false;
  return !holderPreview && !previewRoute;
}
