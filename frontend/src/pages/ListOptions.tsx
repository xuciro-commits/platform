import { BottomSheet } from "@astryxdesign/core/BottomSheet";
import { Button } from "@astryxdesign/core/Button";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { Item } from "@astryxdesign/core/Item";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { List } from "@astryxdesign/core/List";
import { Popover } from "@astryxdesign/core/Popover";
import { RadioList, RadioListItem } from "@astryxdesign/core/RadioList";
import { Section } from "@astryxdesign/core/Section";
import { Tab, TabList } from "@astryxdesign/core/TabList";
import { Heading, Text } from "@astryxdesign/core/Text";
import { VisuallyHidden } from "@astryxdesign/core/VisuallyHidden";
import { useAnnounce } from "@astryxdesign/core/hooks";
import { borderVars, colorVars, durationVars, easeVars, radiusVars, spacingVars, typeScaleVars } from "@astryxdesign/core/theme/tokens.stylex";
import { Bars3Icon, ChevronDownIcon, ChevronUpDownIcon, LockClosedIcon, PlusIcon, RectangleStackIcon, ViewColumnsIcon, XMarkIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { type KeyboardEvent, type PointerEvent, useId, useMemo, useRef, useState } from "react";
import { t } from "../i18n";
import type { Density, StickyEdge, ViewState } from "./view";

type Pane = "columns" | "density" | "sticky" | "grouping";
type Reorder = { key: string; mode: "keyboard" | "pointer"; original: string[]; from: number; to: number; pointer?: number; startY?: number; moved?: boolean };

const sections: { key: Pane; label: string; icon: typeof Bars3Icon }[] = [
  { key: "columns", label: "Columns", icon: ViewColumnsIcon },
  { key: "density", label: "Density", icon: Bars3Icon },
  { key: "sticky", label: "Frozen columns", icon: LockClosedIcon },
  { key: "grouping", label: "Grouping", icon: RectangleStackIcon },
];
const densities: { value: Density; label: string }[] = [
  { value: "compact", label: "Compact" },
  { value: "balanced", label: "Balanced" },
  { value: "spacious", label: "Spacious" },
];
const startEdges: { value: StickyEdge; label: string }[] = [
  { value: "none", label: "None" },
  { value: "one", label: "First column" },
  { value: "two", label: "First two columns" },
];
const endEdges: { value: StickyEdge; label: string }[] = [
  { value: "none", label: "None" },
  { value: "one", label: "Last column" },
  { value: "two", label: "Last two columns" },
];
const dragThreshold = 5;

const indicator = {
  backgroundColor: colorVars["--color-accent"],
  borderRadius: radiusVars["--radius-full"],
  content: '""',
  height: spacingVars["--spacing-0-5"],
  insetInline: 0,
  pointerEvents: "none" as const,
  position: "absolute" as const,
  zIndex: 2,
};

const styles = stylex.create({
  surface: { paddingBlockStart: 0, paddingBlockEnd: 0, paddingInlineStart: 0, paddingInlineEnd: 0 },
  popover: { blockSize: 448 },
  rail: { width: 184, flexShrink: 0 },
  railItem: { width: "100%", justifyContent: "flex-start" },
  sheetTabs: { paddingInline: 16 },
  panel: { blockSize: "100%", flexGrow: 1, minWidth: 0 },
  root: { flexGrow: 1, minHeight: 0, minWidth: 0 },
  panels: { flexGrow: 1, minHeight: 0, minWidth: 0 },
  half: { flexBasis: 0, flexGrow: 1, minBlockSize: 0, minWidth: 0, overflow: "hidden" },
  padStart: { paddingInlineEnd: spacingVars["--spacing-3"], paddingInlineStart: spacingVars["--spacing-4"] },
  padEnd: { paddingInlineEnd: spacingVars["--spacing-4"], paddingInlineStart: spacingVars["--spacing-3"] },
  divider: { borderInlineStartColor: colorVars["--color-border"], borderInlineStartStyle: "solid", borderInlineStartWidth: borderVars["--border-width"] },
  header: { borderBlockEndColor: colorVars["--color-border"], borderBlockEndStyle: "solid", borderBlockEndWidth: borderVars["--border-width"] },
  headerAction: {
    backgroundImage: { default: "none", ":hover:where(:not(:disabled,[aria-disabled=\"true\"]))": "none", ":active": "none" },
    borderRadius: 0,
    color: colorVars["--color-text-accent"],
    fontSize: typeScaleVars["--text-body-size"],
    fontWeight: typeScaleVars["--text-body-weight"],
    height: "auto",
    minHeight: 0,
    lineHeight: typeScaleVars["--text-body-leading"],
    paddingBlock: 0,
    paddingInline: 0,
    textDecoration: { default: "none", ":hover:where(:not(:disabled,[aria-disabled=\"true\"]))": "underline" },
  },
  body: { flexGrow: 1, minBlockSize: 0, overflowY: "auto", overscrollBehavior: "contain", scrollbarGutter: "stable" },
  item: {
    minWidth: 0,
    transitionDuration: { default: durationVars["--duration-fast"], "@media (prefers-reduced-motion: reduce)": "0s" },
    transitionProperty: "background-color, opacity",
    transitionTimingFunction: easeVars["--ease-standard"],
  },
  picked: { backgroundColor: colorVars["--color-accent-muted"] },
  empty: { textAlign: "center" },
  grip: { marginInlineStart: `calc(-1 * ${spacingVars["--spacing-1-5"]})` },
  endAction: { marginInlineEnd: `calc(-1 * ${spacingVars["--spacing-1-5"]})` },
});

const reorderStyles = stylex.create({
  source: { opacity: 0.5, userSelect: "none" },
  before: { position: "relative", "::before": { ...indicator, insetBlockStart: `calc(-1 * ${spacingVars["--spacing-0-5"]})` } },
  after: { position: "relative", "::after": { ...indicator, insetBlockEnd: `calc(-1 * ${spacingVars["--spacing-0-5"]})` } },
  handle: { cursor: "grab", touchAction: "none" },
  active: { cursor: "grabbing", touchAction: "none" },
});

type ListOptionsProps = {
  view: ViewState;
  update: (next: Partial<ViewState>) => void;
  columns: { key: string; header: string }[];
  locked: string;
  defaults: string[];
  groups: { key: string; label: string }[];
  onGroup: (key: string) => void;
  compact: boolean;
};

export function ListOptions({ view, update, columns, locked, defaults, groups, onGroup, compact }: ListOptionsProps) {
  const [section, setSection] = useState<Pane>("columns");
  const [open, setOpen] = useState(false);
  const [reorder, setReorderState] = useState<Reorder | null>(null);
  const reorderRef = useRef<Reorder | null>(null);
  const rows = useRef(new Map<string, HTMLElement>());
  const suppress = useRef(false);
  const announce = useAnnounce();
  const hintId = useId();
  const panelId = useId();
  const labelOf = (key: string) => t(columns.find((column) => column.key === key)?.header ?? key);
  const displayed = view.columns;
  const available = columns.filter((column) => !displayed.includes(column.key)).map((column) => column.key);
  const setReorder = (next: Reorder | null) => {
    reorderRef.current = next;
    setReorderState(next);
  };
  const commit = (keys: string[]) => update({ columns: [...keys] });
  const move = (key: string, requested: number) => {
    const from = displayed.indexOf(key);
    const to = Math.max(0, Math.min(displayed.length - 1, requested));
    if (from < 0 || to === from) return false;
    const next = [...displayed];
    next.splice(from, 1);
    next.splice(to, 0, key);
    commit(next);
    return true;
  };
  const position = (index: number) => `${t("position")} ${index + 1} / ${displayed.length}`;
  const gripClick = (key: string) => {
    if (suppress.current) {
      suppress.current = false;
      return;
    }
    const index = displayed.indexOf(key);
    if (reorderRef.current?.key === key) {
      setReorder(null);
      announce(`${labelOf(key)} ${t("dropped")}, ${position(index)}`);
      return;
    }
    setReorder({ key, mode: "keyboard", original: [...displayed], from: index, to: index });
    announce(`${labelOf(key)} ${t("picked up")}, ${position(index)}`);
  };
  const gripKey = (event: KeyboardEvent<HTMLButtonElement>, key: string) => {
    const session = reorderRef.current;
    if (event.key === "Escape" && session?.key === key) {
      event.preventDefault();
      event.stopPropagation();
      if (session.original.join() !== displayed.join()) commit(session.original);
      setReorder(null);
      announce(`${labelOf(key)} ${t("move cancelled")}`);
      return;
    }
    const index = displayed.indexOf(key);
    const target = ({ ArrowUp: index - 1, ArrowDown: index + 1, Home: 0, End: displayed.length - 1 } as Record<string, number>)[event.key];
    if (target == null) return;
    event.preventDefault();
    if (move(key, target)) announce(`${labelOf(key)}, ${position(Math.max(0, Math.min(displayed.length - 1, target)))}`);
  };
  const gripDown = (event: PointerEvent<HTMLButtonElement>, key: string) => {
    if (event.button !== 0 || !rows.current.get(key)) return;
    suppress.current = true;
    event.currentTarget.setPointerCapture?.(event.pointerId);
    const index = displayed.indexOf(key);
    setReorder({ key, mode: "pointer", original: [...displayed], from: index, to: index, pointer: event.pointerId, startY: event.clientY, moved: false });
  };
  const gripMove = (event: PointerEvent<HTMLButtonElement>) => {
    const session = reorderRef.current;
    if (session?.mode !== "pointer" || session.pointer !== event.pointerId) return;
    if (!session.moved && Math.abs(event.clientY - (session.startY ?? 0)) < dragThreshold) return;
    const remaining = session.original.filter((key) => key !== session.key);
    let to = remaining.length;
    for (let index = 0; index < remaining.length; index += 1) {
      const bounds = rows.current.get(remaining[index])?.getBoundingClientRect();
      if (bounds && event.clientY < bounds.top + bounds.height / 2) {
        to = index;
        break;
      }
    }
    if (session.moved && session.to === to) return;
    setReorder({ ...session, moved: true, to });
  };
  const gripEnd = (event: PointerEvent<HTMLButtonElement>, cancelled: boolean) => {
    const session = reorderRef.current;
    if (session?.mode !== "pointer" || session.pointer !== event.pointerId) return;
    if (event.currentTarget.hasPointerCapture?.(event.pointerId)) event.currentTarget.releasePointerCapture?.(event.pointerId);
    setReorder(null);
    if (cancelled || !session.moved) {
      suppress.current = false;
      return;
    }
    setTimeout(() => {
      suppress.current = false;
    }, 0);
    if (session.to === session.from) return;
    const next = [...session.original];
    next.splice(session.from, 1);
    next.splice(session.to, 0, session.key);
    commit(next);
    announce(`${labelOf(session.key)} ${t("dropped")}, ${position(session.to)}`);
  };
  const drop = useMemo(() => {
    if (reorder?.mode !== "pointer" || !reorder.moved || reorder.to === reorder.from) return null;
    const remaining = reorder.original.filter((key) => key !== reorder.key);
    const before = remaining[reorder.to];
    if (before != null) return { key: before, position: "before" as const };
    const after = remaining.at(-1);
    return after == null ? null : { key: after, position: "after" as const };
  }, [reorder]);

  const columnsPanel = (
    <VStack gap={0} minHeight={0} xstyle={styles.root}>
      <VisuallyHidden id={hintId}>{t("Press Space or Enter to pick up a column. Use the arrow keys to move it, Space or Enter to drop, or Escape to cancel.")}</VisuallyHidden>
      <HStack gap={0} minHeight={0} xstyle={styles.panels}>
        <VStack gap={0} role="group" aria-labelledby={`${hintId}-shown`} xstyle={styles.half}>
          <HStack gap={2} vAlign="center" hAlign="between" paddingBlock={2} xstyle={[styles.header, styles.padStart]}>
            <Text id={`${hintId}-shown`} type="label" color="secondary">
              {t("Displayed columns")}
            </Text>
            <Button label={t("Restore")} variant="ghost" size="sm" xstyle={styles.headerAction} onClick={() => commit(defaults)} />
          </HStack>
          <VStack gap={0} xstyle={styles.body}>
            <List density="compact" header={<VisuallyHidden>{t("Displayed columns")}</VisuallyHidden>}>
              {displayed.map((key) => {
                const picked = reorder?.key === key && reorder.mode === "keyboard";
                const dragging = reorder?.key === key && reorder.mode === "pointer" && reorder.moved;
                const place = drop?.key === key ? drop.position : null;
                return (
                  <Item
                    key={key}
                    as="li"
                    density="compact"
                    ref={(node: HTMLElement | null) => {
                      if (node) rows.current.set(key, node);
                      else rows.current.delete(key);
                    }}
                    label={labelOf(key)}
                    startContent={
                      <IconButton
                        label={`${t("Reorder")} ${labelOf(key)}`}
                        aria-describedby={hintId}
                        aria-pressed={picked}
                        variant="ghost"
                        size="sm"
                        icon={<Icon icon={ChevronUpDownIcon} size="sm" />}
                        xstyle={[styles.grip, reorderStyles.handle, (picked || dragging) && reorderStyles.active]}
                        onClick={() => gripClick(key)}
                        onKeyDown={(event) => gripKey(event, key)}
                        onPointerDown={(event) => gripDown(event, key)}
                        onPointerMove={gripMove}
                        onPointerUp={(event) => gripEnd(event, false)}
                        onPointerCancel={(event) => gripEnd(event, true)}
                        onLostPointerCapture={(event) => gripEnd(event, true)}
                      />
                    }
                    endContent={
                      <IconButton
                        label={`${t("Remove")} ${labelOf(key)}`}
                        variant="ghost"
                        size="sm"
                        isDisabled={key === locked}
                        tooltip={key === locked ? t("This column names every row, so it cannot be removed.") : undefined}
                        icon={<Icon icon={XMarkIcon} size="sm" />}
                        xstyle={styles.endAction}
                        onClick={() => commit(displayed.filter((candidate) => candidate !== key))}
                      />
                    }
                    xstyle={[styles.item, styles.padStart, picked && styles.picked, dragging && reorderStyles.source, place === "before" && reorderStyles.before, place === "after" && reorderStyles.after]}
                  />
                );
              })}
            </List>
          </VStack>
        </VStack>
        <VStack gap={0} role="group" aria-labelledby={`${hintId}-available`} xstyle={[styles.half, styles.divider]}>
          <HStack gap={2} vAlign="center" hAlign="between" paddingBlock={2} xstyle={[styles.header, styles.padEnd]}>
            <Text id={`${hintId}-available`} type="label" color="secondary">
              {t("Available columns")}
            </Text>
            <Button label={t("Select all")} variant="ghost" size="sm" xstyle={styles.headerAction} isDisabled={available.length === 0} onClick={() => commit([...displayed, ...available])} />
          </HStack>
          <VStack gap={0} xstyle={styles.body}>
            {available.length === 0 ? (
              <VStack gap={0} vAlign="center" hAlign="center" minHeight="100%" paddingBlock={4} xstyle={[styles.empty, styles.padEnd]}>
                <Text type="supporting" color="secondary">
                  {t("All columns are displayed.")}
                </Text>
              </VStack>
            ) : (
              <List density="compact" header={<VisuallyHidden>{t("Available columns")}</VisuallyHidden>}>
                {available.map((key) => (
                  <Item
                    key={key}
                    as="li"
                    density="compact"
                    label={labelOf(key)}
                    endContent={
                      <IconButton label={`${t("Add")} ${labelOf(key)}`} tooltip={`${t("Add")} ${labelOf(key)}`} variant="ghost" size="sm" icon={<Icon icon={PlusIcon} size="sm" />} xstyle={styles.endAction} onClick={() => commit([...displayed, key])} />
                    }
                    xstyle={[styles.item, styles.padEnd]}
                  />
                ))}
              </List>
            )}
          </VStack>
        </VStack>
      </HStack>
    </VStack>
  );

  const body = () => {
    switch (section) {
      case "columns":
        return columnsPanel;
      case "density":
        return (
          <VStack gap={0} paddingInline={4} paddingBlockEnd={4}>
            <RadioList label={t("Density")} isLabelHidden value={view.density} onChange={(value) => update({ density: value as Density })}>
              {densities.map((option) => (
                <RadioListItem key={option.value} value={option.value} label={t(option.label)} />
              ))}
            </RadioList>
          </VStack>
        );
      case "sticky":
        return (
          <VStack gap={4} paddingInline={4} paddingBlockEnd={4}>
            {[
              { label: "First columns", value: compact ? "none" : view.stickyStart, options: startEdges, field: "stickyStart" as const },
              { label: "Last columns", value: compact ? "none" : view.stickyEnd, options: endEdges, field: "stickyEnd" as const },
            ].map((edge) => (
              <RadioList
                key={edge.field}
                label={t(edge.label)}
                value={edge.value}
                isDisabled={compact}
                disabledMessage={t("Freezing is off on a narrow screen: a pinned column would leave too little width to scroll the rest.")}
                onChange={(value) => update({ [edge.field]: value as StickyEdge })}>
                {edge.options.map((option) => (
                  <RadioListItem key={option.value} value={option.value} label={t(option.label)} />
                ))}
              </RadioList>
            ))}
          </VStack>
        );
      case "grouping":
        return (
          <VStack gap={0} paddingInline={4} paddingBlockEnd={4}>
            <RadioList label={t("Grouping")} isLabelHidden value={view.group} onChange={(value) => onGroup(String(value))}>
              <RadioListItem value="none" label={t("None")} />
              {groups.map((group) => (
                <RadioListItem key={group.key} value={group.key} label={t(group.label)} />
              ))}
            </RadioList>
          </VStack>
        );
    }
  };
  const visible = sections.filter((candidate) => candidate.key !== "grouping" || groups.length > 0);
  const active = visible.find((candidate) => candidate.key === section);
  const trigger = <Button label={t("View options")} variant="ghost" size="sm" endContent={<Icon icon={ChevronDownIcon} size="sm" />} onClick={compact ? () => setOpen(true) : undefined} />;

  if (compact) {
    return (
      <>
        {trigger}
        <BottomSheet isOpen={open} onOpenChange={setOpen} label={t("View options")} height="tall" padding={0}>
          <VStack gap={0}>
            <TabList value={section} onChange={(value) => setSection(value as Pane)} role="tablist" hasDivider xstyle={styles.sheetTabs}>
              {visible.map((candidate) => (
                <Tab key={candidate.key} id={`${panelId}-tab-${candidate.key}`} value={candidate.key} label={t(candidate.label)} icon={<Icon icon={candidate.icon} size="sm" />} panelId={panelId} />
              ))}
            </TabList>
            <VStack gap={0} id={panelId} role="tabpanel" aria-labelledby={`${panelId}-tab-${section}`} paddingBlockStart={3}>
              {body()}
            </VStack>
          </VStack>
        </BottomSheet>
      </>
    );
  }
  return (
    <Popover
      placement="below"
      alignment="end"
      width={660}
      label={t("View options")}
      isOpen={open}
      onOpenChange={setOpen}
      xstyle={styles.surface}
      content={
        <HStack gap={0} xstyle={styles.popover}>
          <Section variant="transparent" padding={1} dividers={["end"]} xstyle={styles.rail}>
            <VStack gap={1}>
              {visible.map((candidate) => (
                <Button
                  key={candidate.key}
                  label={t(candidate.label)}
                  variant={section === candidate.key ? "secondary" : "ghost"}
                  icon={<Icon icon={candidate.icon} size="sm" />}
                  xstyle={styles.railItem}
                  onClick={() => setSection(candidate.key)}
                />
              ))}
            </VStack>
          </Section>
          <StackItem size="fill">
            <VStack gap={0} minHeight={0} xstyle={styles.panel}>
              <VStack gap={0} padding={4} paddingBlockEnd={3}>
                <Heading level={3}>{t(active?.label ?? "")}</Heading>
              </VStack>
              {body()}
            </VStack>
          </StackItem>
        </HStack>
      }>
      {trigger}
    </Popover>
  );
}
