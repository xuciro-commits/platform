import { Banner } from "@astryxdesign/core/Banner";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { Layout, LayoutContent, VStack } from "@astryxdesign/core/Layout";
import { Lightbox } from "@astryxdesign/core/Lightbox";
import { Markdown } from "@astryxdesign/core/Markdown";
import { Spinner } from "@astryxdesign/core/Spinner";
import { radiusVars } from "@astryxdesign/core/theme/tokens.stylex";
import * as stylex from "@stylexjs/stylex";
import pdfWorkerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { requestBlob, reasonOf } from "../host";
import { t } from "../i18n";

export type Media = { path: string; name: string; contentType?: string };
type Kind = "image" | "pdf" | "text";

const styles = stylex.create({
  page: { width: "100%", height: "auto", borderRadius: radiusVars["--radius-element"] },
  loading: { minHeight: 160, alignItems: "center", justifyContent: "center" },
});

const byType: Record<string, Kind> = {
  "image/png": "image", "image/jpeg": "image", "image/webp": "image", "image/gif": "image", "image/avif": "image",
  "application/pdf": "pdf", "text/markdown": "text", "text/plain": "text",
};
const byExtension: Record<string, Kind> = { png: "image", jpg: "image", jpeg: "image", webp: "image", gif: "image", avif: "image", pdf: "pdf", md: "text", txt: "text" };

export function kindOf(media: Pick<Media, "name" | "contentType">): Kind | undefined {
  return (media.contentType && byType[media.contentType.split(";")[0].trim()]) || byExtension[media.name.split(".").pop()?.toLowerCase() ?? ""];
}

function detectSafeMime(kind: Kind, bytes: Uint8Array): string | null {
  if (kind === "pdf") {
    return bytes.length >= 5 && bytes[0] === 0x25 && bytes[1] === 0x50 && bytes[2] === 0x44 && bytes[3] === 0x46 && bytes[4] === 0x2d
      ? "application/pdf"
      : null;
  }
  if (kind === "image") {
    if (bytes.length >= 8 && bytes[0] === 0x89 && bytes[1] === 0x50 && bytes[2] === 0x4e && bytes[3] === 0x47) return "image/png";
    if (bytes.length >= 3 && bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) return "image/jpeg";
    if (bytes.length >= 12 && bytes[0] === 0x52 && bytes[1] === 0x49 && bytes[2] === 0x46 && bytes[3] === 0x46 && bytes[8] === 0x57 && bytes[9] === 0x45 && bytes[10] === 0x42 && bytes[11] === 0x50) return "image/webp";
    if (bytes.length >= 6 && bytes[0] === 0x47 && bytes[1] === 0x49 && bytes[2] === 0x46 && bytes[3] === 0x38) return "image/gif";
    if (bytes.length >= 12 && bytes[4] === 0x66 && bytes[5] === 0x74 && bytes[6] === 0x79 && bytes[7] === 0x70) return "image/avif";
    return null;
  }
  return "text/plain";
}

function ImageViewer({ media, blob, onClose }: { media: Media; blob: Blob; onClose: () => void }) {
  const [url] = useState(() => URL.createObjectURL(blob));
  useEffect(() => () => URL.revokeObjectURL(url), [url]);
  return <Lightbox isOpen media={{ src: url, alt: media.name }} onOpenChange={(open) => !open && onClose()} />;
}

function Reader({ media, onClose, children }: { media: Media; onClose: () => void; children: ReactNode }) {
  return (
    <Dialog isOpen onOpenChange={onClose} width={880} maxHeight="88dvh">
      <Layout
        height="fill"
        padding={4}
        header={<DialogHeader title={media.name} onOpenChange={onClose} />}
        content={<LayoutContent isScrollable>{children}</LayoutContent>}
      />
    </Dialog>
  );
}

function PdfContent({ media, blob }: { media: Media; blob: Blob }) {
  const pages = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<{ loading: boolean; failure: string }>({ loading: true, failure: "" });
  useEffect(() => {
    let live = true;
    let task: { destroy(): Promise<void> } | undefined;
    const box = pages.current;
    void (async () => {
      const pdfjs = await import("pdfjs-dist");
      pdfjs.GlobalWorkerOptions.workerSrc = typeof pdfWorkerUrl === "string" && pdfWorkerUrl ? pdfWorkerUrl : new URL("pdfjs-dist/build/pdf.worker.min.mjs", import.meta.url).href;
      const loading = pdfjs.getDocument({
        data: new Uint8Array(await blob.arrayBuffer()),
        enableXfa: false,
        disableFontFace: true,
        disableAutoFetch: true,
        disableStream: true,
      });
      task = loading;
      const document = await loading.promise;
      const width = (box?.clientWidth || 800) * window.devicePixelRatio;
      for (let number = 1; number <= document.numPages && live; number += 1) {
        const page = await document.getPage(number);
        const viewport = page.getViewport({ scale: width / page.getViewport({ scale: 1 }).width });
        const canvas = window.document.createElement("canvas");
        Object.assign(canvas, { width: viewport.width, height: viewport.height, className: stylex.props(styles.page).className ?? "" });
        canvas.setAttribute("aria-label", `${t("Page")} ${number}`);
        await page.render({ canvas, viewport }).promise;
        if (live) box?.append(canvas);
        if (number === 1 && live) setState({ loading: false, failure: "" });
      }
    })().catch((problem: unknown) => live && setState({ loading: false, failure: reasonOf(problem) }));
    return () => {
      live = false;
      void task?.destroy();
      box?.replaceChildren();
    };
  }, [blob]);
  return (
    <>
      {state.loading && <VStack xstyle={styles.loading}><Spinner /></VStack>}
      {state.failure && <Banner status="error" title={t("This file could not be shown")} description={state.failure} />}
      <VStack ref={pages} gap={4} role="document" aria-label={media.name} />
    </>
  );
}

export function MediaPreview({ media, onClose, onFailure }: { media: Media; onClose: () => void; onFailure: (reason: string) => void }) {
  const [blob, setBlob] = useState<Blob>();
  const [text, setText] = useState<string>();
  const kind = kindOf(media);
  const callbacks = useRef({ onClose, onFailure });
  callbacks.current = { onClose, onFailure };
  useEffect(() => {
    if (!kind) return;
    setBlob(undefined);
    setText(undefined);
    const controller = new AbortController();
    requestBlob(media.path, controller.signal).then(
      async (answer) => {
        if (controller.signal.aborted) return;
        if (kind === "text") {
          const content = await answer.text();
          if (!controller.signal.aborted) {
            setBlob(answer);
            setText(content);
          }
          return;
        }
        const bytes = new Uint8Array(await answer.arrayBuffer());
        if (controller.signal.aborted) return;
        const safeMime = detectSafeMime(kind, bytes);
        if (!safeMime) {
          callbacks.current.onFailure(t("This file could not be shown"));
          callbacks.current.onClose();
          return;
        }
        setBlob(new Blob([bytes], { type: safeMime }));
      },
      (problem) => {
        if (controller.signal.aborted) return;
        callbacks.current.onFailure(reasonOf(problem));
        callbacks.current.onClose();
      },
    );
    return () => controller.abort();
  }, [media.path, kind]);
  if (!kind) return null;
  if (kind === "image") return blob ? <ImageViewer media={media} blob={blob} onClose={onClose} /> : null;
  return (
    <Reader media={media} onClose={onClose}>
      {kind === "pdf"
        ? (blob ? <PdfContent media={media} blob={blob} /> : <VStack xstyle={styles.loading}><Spinner /></VStack>)
        : (text !== undefined ? <Markdown>{text}</Markdown> : <VStack xstyle={styles.loading}><Spinner /></VStack>)}
    </Reader>
  );
}
