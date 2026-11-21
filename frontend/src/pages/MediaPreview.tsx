import { Banner } from "@astryxdesign/core/Banner";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { Layout, LayoutContent, VStack } from "@astryxdesign/core/Layout";
import { Lightbox } from "@astryxdesign/core/Lightbox";
import { Markdown } from "@astryxdesign/core/Markdown";
import { Spinner } from "@astryxdesign/core/Spinner";
import { radiusVars } from "@astryxdesign/core/theme/tokens.stylex";
import * as stylex from "@stylexjs/stylex";
import { type ComponentType, type ReactNode, useEffect, useRef, useState } from "react";
import { requestBlob } from "../host";
import { t } from "../i18n";
import { reasonOf } from "./act";

export type Media = { path: string; name: string; contentType?: string };
type Kind = "image" | "pdf" | "text";
type Viewer = ComponentType<{ media: Media; blob: Blob; onClose: () => void }>;

const styles = stylex.create({
  page: { width: "100%", height: "auto", borderRadius: radiusVars["--radius-element"] },
});

const byType: Record<string, Kind> = {
  "image/png": "image", "image/jpeg": "image", "image/webp": "image", "image/gif": "image", "image/avif": "image",
  "application/pdf": "pdf", "text/markdown": "text", "text/plain": "text",
};
const byExtension: Record<string, Kind> = { png: "image", jpg: "image", jpeg: "image", webp: "image", gif: "image", avif: "image", pdf: "pdf", md: "text", txt: "text" };

export function kindOf(media: Pick<Media, "name" | "contentType">): Kind | undefined {
  return (media.contentType && byType[media.contentType.split(";")[0].trim()]) || byExtension[media.name.split(".").pop()?.toLowerCase() ?? ""];
}

function ImageViewer({ media, blob, onClose }: { media: Media; blob: Blob; onClose: () => void }) {
  const [url] = useState(() => URL.createObjectURL(blob));
  useEffect(() => () => URL.revokeObjectURL(url), [url]);
  return <Lightbox isOpen media={{ src: url, alt: media.name }} onOpenChange={(open) => !open && onClose()} />;
}

function Reader({ media, onClose, children }: { media: Media; onClose: () => void; children: ReactNode }) {
  return (
    <Dialog isOpen onOpenChange={onClose} width={880} maxHeight="88dvh">
      <Layout height="fill" padding={4} header={<DialogHeader title={media.name} onOpenChange={onClose} />} content={<LayoutContent>{children}</LayoutContent>} />
    </Dialog>
  );
}

function PdfViewer({ media, blob, onClose }: { media: Media; blob: Blob; onClose: () => void }) {
  const pages = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<{ loading: boolean; failure: string }>({ loading: true, failure: "" });
  useEffect(() => {
    let live = true;
    let task: { destroy(): Promise<void> } | undefined;
    const box = pages.current;
    void (async () => {
      const pdfjs = await import("pdfjs-dist");
      pdfjs.GlobalWorkerOptions.workerSrc = (await import("pdfjs-dist/build/pdf.worker.min.mjs?url")).default;
      const loading = pdfjs.getDocument({ data: new Uint8Array(await blob.arrayBuffer()) });
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
    <Reader media={media} onClose={onClose}>
      {state.loading && <Spinner />}
      {state.failure && <Banner status="error" title={t("This file could not be shown")} description={state.failure} />}
      <VStack ref={pages} gap={4} role="document" aria-label={media.name} />
    </Reader>
  );
}

function TextViewer({ media, blob, onClose }: { media: Media; blob: Blob; onClose: () => void }) {
  const [text, setText] = useState<string>();
  useEffect(() => {
    let live = true;
    void blob.text().then((value) => live && setText(value));
    return () => {
      live = false;
    };
  }, [blob]);
  return <Reader media={media} onClose={onClose}>{text === undefined ? <Spinner /> : <Markdown>{text}</Markdown>}</Reader>;
}

const viewers: Record<Kind, Viewer> = { image: ImageViewer, pdf: PdfViewer, text: TextViewer };

export function MediaPreview({ media, onClose, onFailure }: { media: Media; onClose: () => void; onFailure: (reason: string) => void }) {
  const [blob, setBlob] = useState<Blob>();
  const kind = kindOf(media);
  const callbacks = useRef({ onClose, onFailure });
  callbacks.current = { onClose, onFailure };
  useEffect(() => {
    const controller = new AbortController();
    requestBlob(media.path, controller.signal).then(
      (answer) => !controller.signal.aborted && setBlob(answer),
      (problem) => {
        if (controller.signal.aborted) return;
        callbacks.current.onFailure(reasonOf(problem));
        callbacks.current.onClose();
      },
    );
    return () => controller.abort();
  }, [media.path]);
  if (!kind) return null;
  const View = viewers[kind];
  return blob ? <View media={media} blob={blob} onClose={onClose} /> : kind === "image" ? null : <Reader media={media} onClose={onClose}><Spinner /></Reader>;
}
