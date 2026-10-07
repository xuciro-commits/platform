import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { z } from "zod";
import { DataTable, EntityForm, Markdown, MarkdownEditor, NotificationList, RecordLookup, RecordPage, StatusTag, humanizeKernelError, setLanguage, submissionStatuses, type ColumnDef, type RecordSource } from "../index";

afterEach(cleanup);

// jsdom has no layout: give every element a 800×280 box so the virtualizer has a viewport.
Element.prototype.getBoundingClientRect = () => ({ width: 800, height: 280, top: 0, left: 0, right: 800, bottom: 280, x: 0, y: 0, toJSON: () => ({}) });
for (const [key, value] of [["offsetWidth", 800], ["offsetHeight", 280]] as const) {
  Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => value });
}

type Row = { id: string; name: string; qty: number };
const rows: Row[] = Array.from({ length: 100_000 }, (_, i) => ({ id: `r${i}`, name: `Item ${i}`, qty: i % 97 }));
const columns: ColumnDef<Row, any>[] = [
  { accessorKey: "name", header: "Name" },
  { accessorKey: "qty", header: "Qty", meta: { align: "right" } },
];

test("DataTable renders only the visible window of 100,000 rows", () => {
  render(<DataTable data={rows} columns={columns} getRowId={(r) => r.id} height={280} />);
  const rendered = screen.getAllByRole("row").length - 1; // minus header
  expect(rendered).toBeGreaterThan(0);
  expect(rendered).toBeLessThan(40);
  expect(screen.getByText("100,000 rows")).toBeTruthy();
});

test("DataTable filters and sorts", () => {
  render(<DataTable data={rows.slice(0, 50)} columns={columns} getRowId={(r) => r.id} />);
  fireEvent.change(screen.getByLabelText("Filter rows"), { target: { value: "Item 4" } });
  expect(screen.getByText("11 rows")).toBeTruthy(); // Item 4, 40–49
  fireEvent.click(screen.getByRole("button", { name: /Qty/ })); // numbers sort descending first
  expect(screen.getAllByRole("cell")[0]!.textContent).toBe("Item 49");
  fireEvent.click(screen.getByRole("button", { name: /Qty/ }));
  expect(screen.getAllByRole("cell")[0]!.textContent).toBe("Item 4");
});

test("DataTable shows loading state when loading is true and data is empty", () => {
  render(<DataTable data={[]} columns={columns} getRowId={(r) => r.id} loading />);
  expect(screen.getByText("Loading…")).toBeTruthy();
});

test("DataTable shows custom empty text when not loading and data is empty", () => {
  render(<DataTable data={[]} columns={columns} getRowId={(r) => r.id} empty="Nothing here" />);
  expect(screen.getByText("Nothing here")).toBeTruthy();
});

test("DataTable handles cell editing, Escape cancel, Enter commit, and Tab hop", () => {
  const onCellEdit = vi.fn();
  const editableColumns: ColumnDef<Row, any>[] = [
    {
      accessorKey: "name",
      header: "Name",
      meta: {
        field: {
          label: "Name",
          editor: ({ value, onChange }: { value: unknown; onChange: (v: unknown) => void }) => (
            <input
              data-testid="cell-input"
              value={String(value ?? "")}
              onChange={(e) => onChange(e.target.value)}
            />
          ),
        } as any,
      },
    },
    {
      accessorKey: "qty",
      header: "Qty",
    },
  ];

  render(
    <DataTable
      data={rows.slice(0, 5)}
      columns={editableColumns}
      getRowId={(r) => r.id}
      onCellEdit={onCellEdit}
    />
  );

  const firstCell = screen.getAllByRole("cell")[0]!;

  // Double-click to begin editing
  fireEvent.doubleClick(firstCell);
  const input = screen.getByTestId("cell-input") as HTMLInputElement;
  expect(input).toBeTruthy();
  expect(input.value).toBe("Item 0");

  // Type new value
  fireEvent.change(input, { target: { value: "Updated Item 0" } });

  // Escape cancels edit and does not commit
  fireEvent.keyDown(input, { key: "Escape" });
  expect(screen.queryByTestId("cell-input")).toBeNull();
  expect(onCellEdit).not.toHaveBeenCalled();

  // Double-click again, type and press Enter to commit
  fireEvent.doubleClick(firstCell);
  const input2 = screen.getByTestId("cell-input") as HTMLInputElement;
  fireEvent.change(input2, { target: { value: "Committed Item 0" } });
  fireEvent.keyDown(input2, { key: "Enter" });
  expect(onCellEdit).toHaveBeenCalledWith(rows[0], "name", "Committed Item 0");
  expect(screen.queryByTestId("cell-input")).toBeNull();

  // Double-click again, change and press Tab to commit and hop
  fireEvent.doubleClick(firstCell);
  const input3 = screen.getByTestId("cell-input") as HTMLInputElement;
  fireEvent.change(input3, { target: { value: "Tab Item 0" } });
  fireEvent.keyDown(input3, { key: "Tab" });
  expect(onCellEdit).toHaveBeenCalledWith(rows[0], "name", "Tab Item 0");
  expect(screen.queryByTestId("cell-input")).toBeNull();
});

test("RecordLookup finds a scoped record beyond the first 500 without preloading them", async () => {
  const all = Array.from({ length: 600 }, (_, i) => ({ id: `A-${String(i + 1).padStart(3, "0")}`, name: `Account ${i + 1}` }));
  const list = vi.fn(async (_type: string, q: { search?: string; offset?: number; limit?: number }) => {
    const matching = all.filter((r) => !q.search || `${r.id} ${r.name}`.toLowerCase().includes(q.search.toLowerCase()));
    return { records: matching.slice(q.offset ?? 0, (q.offset ?? 0) + (q.limit ?? 25)) as never[], total: matching.length };
  });
  const source = { entity: () => ({ type: "crm.account", display: "name" }), list } as unknown as RecordSource;
  const chosen = vi.fn();
  render(<RecordLookup id="account" source={source} type="crm.account" onChange={chosen} />);
  const input = screen.getByRole("combobox");
  fireEvent.focus(input);
  await waitFor(() => expect(screen.getByRole("option", { name: "A-001 · Account 1" })).toBeTruthy());
  expect(list).toHaveBeenCalledWith("crm.account", expect.objectContaining({ limit: 25, offset: 0 }));
  fireEvent.change(input, { target: { value: "A-599" } });
  await waitFor(() => expect(screen.getByRole("option", { name: "A-599 · Account 599" })).toBeTruthy());
  fireEvent.click(screen.getByRole("option", { name: "A-599 · Account 599" }));
  expect(chosen).toHaveBeenCalledWith("A-599");
  expect(list).toHaveBeenCalledWith("crm.account", expect.objectContaining({ search: "A-599", limit: 25 }));
});

test("StatusTag maps states to tones and falls back to neutral", () => {
  render(<>
    <StatusTag status="SUBMISSION_STATE_CONFLICT" registry={submissionStatuses} />
    <StatusTag status="SOMETHING_NEW" registry={submissionStatuses} />
  </>);
  expect(screen.getByText("Conflict").closest("[data-tone]")!.getAttribute("data-tone")).toBe("danger");
  expect(screen.getByText("SOMETHING_NEW").closest("[data-tone]")!.getAttribute("data-tone")).toBe("neutral");
});

test("EntityForm validates with the schema before submitting", async () => {
  const submit = vi.fn();
  const schema = z.object({ guest: z.string().min(1, "Required"), nights: z.number().min(1, "At least one night") });
  render(<EntityForm schema={schema} onSubmit={submit} defaultValues={{ guest: "", nights: 0 }}
    fields={[{ name: "guest", label: "Guest" }, { name: "nights", label: "Nights", kind: "number" }]} />);
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Save" })); });
  expect(submit).not.toHaveBeenCalled();
  expect(screen.getByText("Required")).toBeTruthy();
  expect(screen.getByLabelText("Guest").getAttribute("aria-invalid")).toBe("true");
  fireEvent.change(screen.getByLabelText("Guest"), { target: { value: "Ada" } });
  fireEvent.change(screen.getByLabelText("Nights"), { target: { value: "2" } });
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Save" })); });
  expect(submit).toHaveBeenCalledWith({ guest: "Ada", nights: 2 }, expect.anything());
});

test("EntityForm disables buttons and indicates saving while submitting", async () => {
  let finishSubmit: () => void = () => {};
  const pending = new Promise<void>((resolve) => { finishSubmit = resolve; });
  const submit = vi.fn().mockImplementation(() => pending);
  const cancel = vi.fn();
  const schema = z.object({ title: z.string().min(1) });
  render(<EntityForm schema={schema} onSubmit={submit} onCancel={cancel} defaultValues={{ title: "Hello" }}
    fields={[{ name: "title", label: "Title" }]} />);

  const saveBtn = screen.getByRole("button", { name: "Save" }) as HTMLButtonElement;
  const cancelBtn = screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement;
  expect(saveBtn.disabled).toBe(false);
  expect(cancelBtn.disabled).toBe(false);

  await act(async () => {
    fireEvent.click(saveBtn);
  });

  expect(submit).toHaveBeenCalled();
  const savingBtn = screen.getByRole("button", { name: "Saving…" }) as HTMLButtonElement;
  expect(savingBtn.disabled).toBe(true);
  expect(cancelBtn.disabled).toBe(true);

  await act(async () => {
    finishSubmit();
  });

  expect((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(false);
  expect(cancelBtn.disabled).toBe(false);
});

test("routes round-trip through the URL and name one tab per entity", async () => {
  const { routeFromHash, routeKey, routeToHash } = await import("../shell/route");
  const route = { view: "workOrder", params: { tenant: "plant-1", id: "WO 7/2" } };
  expect(routeFromHash(routeToHash(route))).toEqual(route);
  expect(routeKey({ view: "workOrder", params: { id: "WO 7/2", tenant: "plant-1" } })).toBe(routeKey(route));
  expect(routeFromHash("#/home")).toEqual({ view: "home" });
  expect(routeFromHash("")).toBeUndefined();
});

test("NotificationList marks unread notifications and reads them", () => {
  const read = vi.fn();
  render(<NotificationList onRead={read} items={[
    { id: "n-2", title: "Downtime on CNC-11", body: "Line L1", at: "2026-09-24T08:00:00Z", read: false },
    { id: "n-1", title: "Seen before", at: "2026-09-24T07:00:00Z", read: true },
  ]} />);
  const buttons = screen.getAllByRole("button", { name: "Mark read" });
  expect(buttons).toHaveLength(1);
  fireEvent.click(buttons[0]!);
  expect(read).toHaveBeenCalledWith(expect.objectContaining({ id: "n-2" }));
});

test("humanizeKernelError maps error codes and translates to reader's language", () => {
  expect(humanizeKernelError("ERROR_CODE_CONFLICT")).toBe("The record was modified by another operation. Please refresh and try again.");
  setLanguage("zh-CN");
  expect(humanizeKernelError("ERROR_CODE_CONFLICT")).toBe("该记录已被他人修改，请刷新后重试。");
  expect(humanizeKernelError("ERROR_CODE_POLICY_DENIED")).toBe("你无权执行此操作。");
  expect(humanizeKernelError("Custom error message")).toBe("Custom error message");
  setLanguage("en");
});

test("RecordPage shows structured error state with retry on fetch failure", async () => {
  let callCount = 0;
  const mockSource: RecordSource = {
    entity: () => ({ app: "crm", type: "crm.account", title: "Account", plural: "Accounts", display: "id", fields: [], standard: [] }),
    get: vi.fn().mockImplementation(() => {
      callCount++;
      if (callCount === 1) return Promise.reject(new Error("ERROR_CODE_NOT_FOUND"));
      return Promise.resolve({
        record: { id: "ACC-1", type: "crm.account", revision: 1, created: { at: "2026-09-01T00:00:00Z" }, changed: { at: "2026-09-01T00:00:00Z" } },
        tasks: [], approvals: [], processes: [], files: [], comments: [], related: [],
      });
    }),
    list: vi.fn() as any,
    aggregate: vi.fn() as any,
  };

  render(<RecordPage source={mockSource} type="crm.account" id="ACC-1" />);

  await waitFor(() => {
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(screen.getByText("Could not load record.")).toBeTruthy();
    expect(screen.getByText("The record was not found or is outside your scope.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
  });

  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
  });

  await waitFor(() => {
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByText("ACC-1")).toBeTruthy();
  });
});

test("Markdown renders headings, formatting, lists, code blocks and links safely", () => {
  const content = `# Title\n\n## Subtitle\n\n**bold text** and *italic text* and \`code snippet\`\n\n[Documentation](https://example.com/docs)\n\n> This is a quote\n\n- item 1\n- item 2\n\n\`\`\`js\nconst x = 42;\n\`\`\``;
  const { container } = render(<Markdown content={content} />);
  expect(screen.getByRole("heading", { level: 1, name: "Title" })).toBeTruthy();
  expect(screen.getByRole("heading", { level: 2, name: "Subtitle" })).toBeTruthy();
  expect(screen.getByText("bold text")).toBeTruthy();
  expect(screen.getByText("italic text")).toBeTruthy();
  expect(screen.getByText("code snippet")).toBeTruthy();
  const link = screen.getByRole("link", { name: "Documentation" }) as HTMLAnchorElement;
  expect(link.href).toBe("https://example.com/docs");
  expect(screen.getByText("This is a quote")).toBeTruthy();
  expect(screen.getByText("item 1")).toBeTruthy();
  expect(screen.getByText("item 2")).toBeTruthy();
  expect(container.querySelector("pre code")?.textContent).toBe("const x = 42;");
});

test("MarkdownEditor switches between Write and Preview tabs", () => {
  const onChange = vi.fn();
  render(<MarkdownEditor value="# Hello World" onChange={onChange} />);

  expect(screen.getByRole("tab", { name: "Write", selected: true })).toBeTruthy();
  expect(screen.getByDisplayValue("# Hello World")).toBeTruthy();
  expect(screen.getByText("13 characters")).toBeTruthy();

  fireEvent.click(screen.getByRole("tab", { name: "Preview" }));
  expect(screen.getByRole("tab", { name: "Preview", selected: true })).toBeTruthy();
  expect(screen.getByRole("heading", { level: 1, name: "Hello World" })).toBeTruthy();
  expect(screen.queryByDisplayValue("# Hello World")).toBeNull();

  fireEvent.click(screen.getByRole("tab", { name: "Write" }));
  expect(screen.getByDisplayValue("# Hello World")).toBeTruthy();
});

test("EntityForm renders helper text and read-only field states", () => {
  const schema = z.object({
    title: z.string(),
    content: z.string(),
    status: z.string(),
  });
  render(
    <EntityForm
      schema={schema}
      onSubmit={vi.fn()}
      defaultValues={{ title: "Doc 1", content: "Details", status: "Published" }}
      fields={[
        { name: "title", label: "Title", help: "Give the document a descriptive name" },
        { name: "content", label: "Content", kind: "longText", help: "Markdown supported" },
        { name: "status", label: "Status", readOnly: true },
      ]}
    />
  );

  expect(screen.getByText("Give the document a descriptive name")).toBeTruthy();
  expect(screen.getByText("Markdown supported")).toBeTruthy();
  expect(screen.getByText("Read only")).toBeTruthy();
  expect(screen.getByText("Published")).toBeTruthy();
});


