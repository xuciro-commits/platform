import { defaultRangeExtractor, useVirtualizer } from "@tanstack/react-virtual";
import { useRef, useState, type ReactNode } from "react";

/** Measured, identity-keyed rows. A focused row stays mounted while its
 * controls (including portals) own focus. Offscreen local input is disposable. */
export function VirtualStack<T>({ items, itemKey, renderItem, label, height = 560 }: {
  items: readonly T[]; itemKey: (item: T) => string; renderItem: (item: T, index: number) => ReactNode; label: string; height?: number;
}) {
  const scroll = useRef<HTMLDivElement>(null), [focused, setFocused] = useState<string>();
  const keys = items.map(itemKey), pinned = focused ? keys.indexOf(focused) : -1;
  const virtual = useVirtualizer({ count: items.length, getScrollElement: () => scroll.current,
    getItemKey: (index) => keys[index]!, estimateSize: () => 240, overscan: 2,
    initialRect: { width: 0, height },
    rangeExtractor: (range) => { const indices = defaultRangeExtractor(range); return pinned >= 0 ? [...new Set([...indices, pinned])].sort((a,b) => a-b) : indices; },
  });
  return <div ref={scroll} role="list" aria-label={label} tabIndex={0} className="min-w-0 overflow-auto rounded-md border border-border p-2" style={{ height }}
    onFocusCapture={(event) => { const item = (event.target as HTMLElement).closest<HTMLElement>("[data-stack-item]"); if (item && scroll.current?.contains(item)) setFocused(item.dataset.stackItem); else if (event.target === event.currentTarget) setFocused(undefined); }}>
    <div className="relative w-full" style={{ height: virtual.getTotalSize() }}>
      {virtual.getVirtualItems().map((row) => <div key={row.key} ref={virtual.measureElement} data-index={row.index} data-stack-item={keys[row.index]} role="listitem" aria-posinset={row.index+1} aria-setsize={items.length}
        className="absolute left-0 top-0 w-full min-w-0 pb-3" style={{ transform: `translateY(${row.start}px)` }}>{renderItem(items[row.index]!, row.index)}</div>)}
    </div>
  </div>;
}
