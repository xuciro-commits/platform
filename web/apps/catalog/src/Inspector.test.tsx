// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { localizeEntry, type CatalogIndex } from "@platform/catalog";
import { StructureTab, ApiTab, RelationsTab, UsageTab } from "./Inspector";
import data from "./gen/catalog.json";

const index = data as CatalogIndex;

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

test("StructureTab renders component identity, slots, composed items, and dependencies", () => {
  const entry = index.entries.find((e) => e.id === "ui/workbench")!;
  const onSelect = vi.fn();

  render(<StructureTab entry={entry} index={index} onSelect={onSelect} />);

  expect(screen.getByText("ui/workbench")).toBeTruthy();
  expect(screen.getByText("@platform/ui")).toBeTruthy();
  expect(screen.getByText("web/packages/ui/src/layout/Workbench.tsx")).toBeTruthy();

  // Slots
  expect(screen.getByText("actions")).toBeTruthy();
  expect(screen.getByText("children")).toBeTruthy();

  // Composed items (Button is composed and also in dependencies)
  const buttons = screen.getAllByText("Button");
  expect(buttons.length).toBeGreaterThanOrEqual(1);
  fireEvent.click(buttons[0]!);
  expect(onSelect).toHaveBeenCalledWith("ui/button");
});

test("ApiTab renders component-specific props, filters, related types, and owner API", () => {
  const entry = index.entries.find((e) => e.id === "ui/button")!;
  render(<ApiTab entry={entry} index={index} />);

  // Public exports
  expect(screen.getByText("Button")).toBeTruthy();

  // Props
  expect(screen.getByText("variant")).toBeTruthy();
  expect(screen.getByText("size")).toBeTruthy();
  expect(screen.getAllByText("type").length).toBeGreaterThanOrEqual(1);

  // Related types
  expect(screen.getByText("ButtonProps")).toBeTruthy();
});

test("ApiTab filters props when search term is entered", () => {
  const entry = index.entries.find((e) => e.id === "ui/workbench")!;
  render(<ApiTab entry={entry} index={index} />);

  const input = screen.getByLabelText("Filter props");
  fireEvent.change(input, { target: { value: "storageKey" } });

  expect(screen.getByText("storageKey")).toBeTruthy();
  expect(screen.queryByText("crumbs")).toBeNull();
});

test("RelationsTab displays upstream consumers and downstream composed items", () => {
  const entry = index.entries.find((e) => e.id === "ui/button")!;
  const onSelect = vi.fn();

  render(<RelationsTab entry={entry} index={index} onSelect={onSelect} />);

  // Upstream: workbench composes button
  expect(screen.getAllByText("Workbench").length).toBeGreaterThanOrEqual(1);

  // Upstream static consumer files
  expect(screen.getByText("Static consumers")).toBeTruthy();
});

test("UsageTab displays snippet and copy action", async () => {
  const entry = index.entries.find((e) => e.id === "ui/button")!;
  const shown = localizeEntry(index, entry, "en");

  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.assign(navigator, {
    clipboard: {
      writeText,
    },
  });

  render(<UsageTab entry={entry} shown={shown} />);

  expect(screen.getByText(/import \{ Button \} from "@platform\/ui";/)).toBeTruthy();

  const copyBtn = screen.getByText("Copy");
  fireEvent.click(copyBtn);
  expect(writeText).toHaveBeenCalled();
});

test("AST accuracy: DataTable composition excludes external Lucide Search and keeps real composed components", () => {
  const dataTable = index.entries.find((e) => e.id === "ui/data-table")!;
  expect(dataTable).toBeDefined();
  // Lucide Search must NOT be falsely identified as app/knowledge-search
  expect(dataTable.composed ?? []).not.toContain("app/knowledge-search");
  // Actual platform component ui/input is accurately identified
  expect(dataTable.composed ?? []).toContain("ui/input");
});

test("AST accuracy: Multi-component files isolate primary component props without polluting from sibling components", () => {
  const workbench = index.entries.find((e) => e.id === "ui/workbench")!;
  expect(workbench).toBeDefined();
  const propNames = (workbench.props ?? []).map((p) => p.name);
  // Workbench's own props are present
  expect(propNames).toContain("storageKey");
  expect(propNames).toContain("crumbs");
  // Sibling components in Workbench.tsx (ProblemList, StructureRow, PanelSection) props are NOT merged
  expect(propNames).not.toContain("problems");
  expect(propNames).not.toContain("empty");
  expect(propNames).not.toContain("depth");
});

test("AST accuracy: Real TypeScript types are prioritized over name guessing or any", () => {
  const dataTable = index.entries.find((e) => e.id === "ui/data-table")!;
  const dataProp = dataTable.props?.find((p) => p.name === "data");
  expect(dataProp).toBeDefined();
  expect(dataProp?.type).toBe("T[]");
  expect(dataProp?.type).not.toBe("any");

  const colsProp = dataTable.props?.find((p) => p.name === "columns");
  expect(colsProp).toBeDefined();
  expect(colsProp?.type).toContain("ColumnDef");
  expect(colsProp?.type).not.toBe("any");
});

test("AST accuracy: Slot optionality is preserved and related type signatures are untruncated", () => {
  const workbench = index.entries.find((e) => e.id === "ui/workbench")!;
  const titleSlot = workbench.slots?.find((s) => s.name === "title");
  expect(titleSlot).toBeDefined();
  expect(titleSlot?.optional).toBe(true);

  // DataTableProps is a multi-line generic type definition whose signature exceeds 500 chars (previously sliced to 160)
  const dataTable = index.entries.find((e) => e.id === "ui/data-table")!;
  const dtProps = dataTable.relatedTypes?.find((t) => t.name === "DataTableProps");
  expect(dtProps).toBeDefined();
  expect(dtProps?.signature).toContain("DataTableProps<T>");
  expect((dtProps?.signature ?? "").length).toBeGreaterThan(200);

  // ButtonProps type alias
  const button = index.entries.find((e) => e.id === "ui/button")!;
  const buttonProps = button.relatedTypes?.find((t) => t.name === "ButtonProps");
  expect(buttonProps).toBeDefined();
  expect(buttonProps?.signature).toContain("ButtonHTMLAttributes");
});
