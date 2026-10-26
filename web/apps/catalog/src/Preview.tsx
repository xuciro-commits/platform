import { Button, t } from "@platform/ui";
import { Component, Suspense, lazy, useEffect, useMemo, useState, type ReactNode } from "react";
import { previewLoaders } from "./gen/previews";

class PreviewBoundary extends Component<{ children: ReactNode; retry: () => void }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() {
    return this.state.failed ? <PreviewFailure retry={this.props.retry} /> : this.props.children;
  }
}

function PreviewFailure({ retry }: { retry: () => void }) {
  return <div role="alert" className="space-y-2 p-3 text-sm">
    <p>{t("This example failed. Other assets remain available.")}</p>
    <Button size="sm" onClick={retry}>{t("Retry example")}</Button>
  </div>;
}

export function CatalogPreview({ id, standalone = false }: { id: string; standalone?: boolean }) {
  const [attempt, setAttempt] = useState(0);
  const [asyncFailure, setAsyncFailure] = useState(false);
  const loader = previewLoaders[id];
  const Example = useMemo(() => loader ? lazy(loader) : undefined, [loader, attempt]);
  const retry = () => { setAsyncFailure(false); setAttempt((n) => n + 1); };
  useEffect(() => {
    // Browser event/promise failures do not pass through React error boundaries.
    const rejected = () => setAsyncFailure(true);
    const failed = (event: ErrorEvent) => { if (event.error) setAsyncFailure(true); };
    addEventListener("unhandledrejection", rejected);
    addEventListener("error", failed);
    return () => { removeEventListener("unhandledrejection", rejected); removeEventListener("error", failed); };
  }, []);
  if (!loader || !Example) return <p className="text-xs text-muted">{t("Reference asset. Follow its source and composed assets.")}</p>;
  if (id === "pattern/workspace" && !standalone) return <div className="space-y-2 text-sm">
    <p>{t("The workspace shell owns its window and navigation. Open its example separately.")}</p>
    <Button onClick={() => { const url = new URL(location.href); url.search = new URLSearchParams({ preview: id }).toString(); url.hash = "";
      window.open(url.href, "_blank", "noopener,noreferrer"); }}>{t("Open isolated example")}</Button>
  </div>;
  return asyncFailure ? <PreviewFailure retry={retry} /> : <PreviewBoundary key={`${id}:${attempt}`} retry={retry}>
    <Suspense fallback={<p className="p-3 text-xs text-muted">{t("Loading example…")}</p>}><Example /></Suspense>
  </PreviewBoundary>;
}
