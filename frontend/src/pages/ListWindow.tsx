import { Section } from "@astryxdesign/core/Section";
import type { TablePlugin } from "@astryxdesign/core/Table";
import { colorVars } from "@astryxdesign/core/theme/tokens.stylex";
import * as stylex from "@stylexjs/stylex";
import { type Ref, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { t } from "../i18n";
import type { Density } from "./view";

type Page = { hasMore: boolean; loading: boolean; loadMore: () => void };

const overscan = 12;
const step = 4;
const heights: Record<Density, number> = { compact: 36, balanced: 48, spacious: 60 };

const styles = stylex.create({
  scroller: { height: "100%", overflow: "auto" },
  header: { position: "sticky", insetBlockStart: 0, zIndex: 2, backgroundColor: colorVars["--color-background-surface"] },
  pinned: { zIndex: 3 },
});

export function useListWindow<T extends Record<string, unknown>>({ count, density, scrollKey, ready, page }: { count: number; density: Density; scrollKey: string; ready: string; page?: Page }) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const innerRef = useRef<Ref<HTMLDivElement>>(undefined);
  const [node, setNode] = useState<HTMLDivElement | null>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewportHeight, setViewportHeight] = useState(600);
  const [rowHeight, setRowHeight] = useState(heights[density]);
  const attach = useCallback((element: HTMLDivElement | null) => {
    scrollRef.current = element;
    setNode(element);
    const inner = innerRef.current;
    if (typeof inner === "function") inner(element);
    else if (inner) inner.current = element;
  }, []);
  const first = Math.max(0, Math.min(count, Math.floor(scrollTop / rowHeight) - overscan));
  const last = Math.min(count, first + Math.ceil(viewportHeight / rowHeight) + overscan * 2);

  useLayoutEffect(() => {
    const counts = new Map<number, number>();
    for (const row of scrollRef.current?.querySelector("tbody")?.rows ?? []) {
      const height = Math.round(row.getBoundingClientRect().height * 2) / 2;
      counts.set(height, (counts.get(height) ?? 0) + 1);
    }
    const measured = [...counts].sort((a, b) => b[1] - a[1])[0]?.[0];
    if (measured && Math.abs(measured - rowHeight) > 0.5) setRowHeight(measured);
  });
  useEffect(() => {
    if (!node) return;
    let restoring = Number(sessionStorage.getItem(scrollKey)) || 0;
    const more = () => page?.hasMore && !page.loading && page.loadMore();
    const onScroll = () => {
      setScrollTop(Math.floor(node.scrollTop / (rowHeight * step)) * rowHeight * step);
      setViewportHeight(node.clientHeight);
      const atEnd = node.scrollHeight - node.scrollTop - node.clientHeight < rowHeight;
      if (restoring > node.scrollTop + rowHeight) {
        if (atEnd) more();
        return;
      }
      restoring = 0;
      sessionStorage.setItem(scrollKey, String(node.scrollTop));
      if (atEnd) more();
    };
    node.addEventListener("scroll", onScroll, { passive: true });
    const frame = requestAnimationFrame(() => {
      setViewportHeight(node.clientHeight);
      const wanted = Math.min(restoring, node.scrollHeight - node.clientHeight);
      node.scrollTop = wanted;
      setScrollTop(wanted);
      if (restoring > wanted + rowHeight) more();
      else if (Math.abs(wanted - restoring) < rowHeight) restoring = 0;
    });
    return () => {
      cancelAnimationFrame(frame);
      node.removeEventListener("scroll", onScroll);
    };
  }, [node, scrollKey, ready, count, rowHeight, page?.hasMore, page?.loading, page?.loadMore]);

  const plugin: TablePlugin<T> = {
    transformHeaderCell: (props) => ({ ...props, xstyle: [...props.xstyle, styles.header, props.htmlProps.style?.position === "sticky" && styles.pinned] }),
    transformScrollWrapper: (props) => {
      innerRef.current = props.htmlProps.ref;
      return {
        ...props,
        htmlProps: { ...props.htmlProps, ref: attach, role: "region", "aria-label": t("Records") },
        xstyle: [...props.xstyle, styles.scroller],
        beforeTable: <>{props.beforeTable}{first > 0 && <Section variant="transparent" height={first * rowHeight} padding={0} />}</>,
        afterTable: <>{last < count && <Section variant="transparent" height={(count - last) * rowHeight} padding={0} />}{props.afterTable}</>,
      };
    },
  };
  return { first, last, plugin };
}
