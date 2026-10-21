import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { z } from "zod";
import type {Api} from "@platform/kernel";
import { DataTable, EntityForm, RecordForm, applyFilters, columnsFor, defineEntity, entityFrom, field, recordSchema } from "../index";

afterEach(cleanup);
Element.prototype.getBoundingClientRect = () => ({ width: 800, height: 280, top: 0, left: 0, right: 800, bottom: 280, x: 0, y: 0, toJSON: () => ({}) });
for (const [key, value] of [["offsetWidth", 800], ["offsetHeight", 280]] as const) {
  Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => value });
}

type Material = { id: string; name: string; kind: string; tags: string[]; price: number; stock: number; supplier?: string; ean?: string };
const material = defineEntity<Material>({
  name: "Material", primary: "id",
  fields: {
    id: field.barcode({ label: "Code", required: true, readOnly: true }),
    name: field.text({ label: "Name", required: true }),
    kind: field.singleSelect({ label: "Kind", options: [{ value: "raw", label: "Raw", tone: "info" }, { value: "part", label: "Part", tone: "success" }] }),
    tags: field.multiSelect({ label: "Tags", options: [{ value: "rohs", label: "RoHS" }, { value: "fragile", label: "Fragile" }] }),
    price: field.currency({ label: "Price", currency: "CNY" }),
    stock: field.number({ label: "Stock", unit: "pcs" }),
    supplier: field.email({ label: "Supplier" }),
    value: field.formula({ label: "Stock value", compute: (m: Material) => m.price * m.stock, as: field.currency({ label: "", currency: "CNY" }) }),
  },
});
const rows: Material[] = [
  { id: "M-1", name: "Aluminium ingot", kind: "raw", tags: ["rohs"], price: 20, stock: 100 },
  { id: "M-2", name: "Valve seat", kind: "part", tags: ["rohs", "fragile"], price: 3.5, stock: 40 },
  { id: "M-3", name: "Gasket", kind: "part", tags: [], price: 0.2, stock: 1000 },
];

test("field types render cells: select tags, currency, units and formulas", () => {
  render(<DataTable data={rows} columns={columnsFor(material)} getRowId={(r) => r.id} height={280} />);
  expect(screen.getAllByText("Part")[0]!.closest("[data-tone]")!.getAttribute("data-tone")).toBe("success");
  expect(screen.getByText("Fragile")).toBeTruthy();
  expect(screen.getByText(/100 pcs/)).toBeTruthy();
  expect(screen.getAllByText(/2,000\.00/).length).toBe(1); // 20 × 100 as stock value
});

test("each field type brings its own filter operators", () => {
  const ids = (filters: Parameters<typeof applyFilters<Material>>[2]) => applyFilters(material, rows, filters).map((r) => r.id);
  expect(ids([{ field: "kind", operator: "is", arg: "part" }])).toEqual(["M-2", "M-3"]);
  expect(ids([{ field: "tags", operator: "has", arg: ["rohs", "fragile"] }])).toEqual(["M-2"]);
  expect(ids([{ field: "stock", operator: "gt", arg: 50 }, { field: "name", operator: "contains", arg: "ingot" }])).toEqual(["M-1"]);
  expect(ids([{ field: "value", operator: "lt", arg: 150 }])).toEqual(["M-2"]);
  expect(ids([{ field: "tags", operator: "empty" }])).toEqual(["M-3"]);
  expect(ids([{ field: "stock", operator: "gt" }])).toEqual(["M-1", "M-2", "M-3"]); // incomplete filters are ignored
});

test("a record validates by its field types", () => {
  const schema = recordSchema(material);
  expect(schema.safeParse({ name: "Bolt", kind: "raw", price: 1, stock: 2 }).success).toBe(true);
  expect(schema.safeParse({ id: "M-9", name: "", kind: "raw" }).success).toBe(false);
  expect(schema.safeParse({ id: "M-9", name: "Bolt", kind: "metal" }).success).toBe(false);
  expect(schema.safeParse({ id: "M-9", name: "Bolt", supplier: "not-an-email" }).success).toBe(false);
  expect(schema.safeParse({ id: "M-9", name: "Bolt", supplier: "" }).success).toBe(true);
});

test("cells edit in place with their field type's editor", () => {
  const edit = vi.fn();
  render(<DataTable data={rows} columns={columnsFor(material)} getRowId={(r) => r.id} height={280} onCellEdit={edit} />);
  const select = vi.spyOn(HTMLInputElement.prototype, "select");
  const stock = screen.getByText(/100 pcs/).closest("[role=cell]")!;
  fireEvent.doubleClick(stock);
  const input = stock.querySelector("input")!;
  expect(select).toHaveBeenCalledTimes(1); // the value is selected: typing replaces it
  fireEvent.change(input, { target: { value: "120" } });
  fireEvent.keyDown(input, { key: "Enter" });
  expect(edit).toHaveBeenCalledWith(rows[0], "stock", 120);
  const value = screen.getAllByText(/2,000\.00/)[0]!.closest("[role=cell]")!;
  fireEvent.doubleClick(value);
  expect(value.querySelector("input")).toBeNull(); // formulas are read-only
  const code = screen.getByText("M-1").closest("[role=cell]")!;
  fireEvent.doubleClick(code);
  expect(code.querySelector("input")).toBeNull(); // so are fields the domain marks read-only
});

test("a record form uses field editors and reports field errors", async () => {
  const submit = vi.fn();
  render(<RecordForm entity={material} keys={["name", "kind", "supplier"]} onSubmit={submit} defaultValues={{ name: "", supplier: "x" }} />);
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Save" })); });
  expect(submit).not.toHaveBeenCalled();
  expect(screen.getByText("Email address")).toBeTruthy();
  fireEvent.change(screen.getByLabelText(/Name/), { target: { value: "Bolt" } });
  fireEvent.change(screen.getByLabelText("Supplier"), { target: { value: "buyer@example.com" } });
  fireEvent.change(screen.getByLabelText("Kind"), { target: { value: "raw" } });
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Save" })); });
  expect(submit).toHaveBeenCalledWith({ name: "Bolt", kind: "raw", supplier: "buyer@example.com" }); // one value: the form never leaks the DOM event
});

test("clearing existing required and optional fields stays visible through validation and refusal", async () => {
  const submit = vi.fn(() => false);
  render(<RecordForm entity={material} keys={["name", "supplier"]} onSubmit={submit}
    defaultValues={{ name: "Bolt", supplier: "buyer@example.com" }} />);
  const name = screen.getByLabelText(/Name/) as HTMLInputElement;
  const supplier = screen.getByLabelText("Supplier") as HTMLInputElement;
  fireEvent.change(name, { target: { value: "" } });
  fireEvent.change(supplier, { target: { value: "" } });
  expect(name.value).toBe("");
  expect(supplier.value).toBe("");
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Save" })); });
  expect(submit).not.toHaveBeenCalled();
  expect(screen.getByText("Enter a value.")).toBeTruthy();
  fireEvent.change(name, { target: { value: "New bolt" } });
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Save" })); });
  expect(submit).toHaveBeenCalledWith({ name: "New bolt", supplier: undefined });
  expect(screen.getByRole("alert").textContent).toBe("The request was not confirmed. Your input is retained.");
  expect(name.value).toBe("New bolt");
  expect(supplier.value).toBe("");
});

test("schema forms explain common validation errors and preserve explicit business messages", async () => {
  const schema = z.object({ name: z.string().min(3), quantity: z.number().min(1), account: z.string().min(3, "Use the registered account name.") });
  render(<EntityForm schema={schema} fields={[{ name: "name", label: "Name" }, { name: "quantity", label: "Quantity", kind: "number" }, { name: "account", label: "Account" }]}
    defaultValues={{ name: "x", quantity: 0, account: "x" }} onSubmit={vi.fn()} />);
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Save" })); });
  expect(screen.getByText("Use at least 3 characters.")).toBeTruthy();
  expect(screen.getByText("Value must be at least 1.")).toBeTruthy();
  expect(screen.getByText("Use the registered account name.")).toBeTruthy();
});

// Words a person types, with no list to choose from: the editor of a tags field
// that carries labels, field names or keywords ('s page composer).
test("a tags field takes words a person types and gives them back", () => {
  const free = field.tags({ label: "Fields in the list" });
  const onChange = vi.fn();
  render(<>{free.editor!({ id: "list", value: ["title"], onChange })}</>);
  const input = screen.getByPlaceholderText("Add…");
  fireEvent.keyDown(input, { key: "Enter", target: { value: "stage" } });
  expect(onChange).toHaveBeenCalledWith(["title", "stage"]);
  fireEvent.click(screen.getByRole("button", { name: "Remove title" }));
  expect(onChange).toHaveBeenLastCalledWith([]);
  expect(free.text(["title", "stage"])).toBe("title stage");
});

test("declared bounds and date relationships share record validation",()=>{
 const start={name:"arrive",type:"date",required:true,description:"Arrival",constraints:{},range:{end:"depart"}};
 const end={name:"depart",type:"date",required:true,description:"Departure",constraints:{after:"arrive"}};
 const entity=defineEntity({name:"stay",primary:"id",fields:{arrive:{...field.date({label:"Arrival",required:true}),input:start},depart:{...field.date({label:"Departure",required:true}),input:end},rooms:{...field.number({label:"Rooms",required:true}),input:{name:"rooms",type:"integer",required:true,description:"Rooms",constraints:{min:1,max:20}}}}});
 const schema=recordSchema(entity);
 expect(schema.safeParse({arrive:"2028-02-29",depart:"2028-03-01",rooms:1}).success).toBe(true);
 for(const values of [{arrive:"2027-02-29",depart:"2028-03-01",rooms:1},{arrive:"2028-02-29",depart:"2028-02-29",rooms:1},{arrive:"2028-02-29",depart:"2028-03-01",rooms:1.5},{arrive:"2028-02-29",depart:"2028-03-01",rooms:21}])expect(schema.safeParse(values).success).toBe(false);
});

test("record forms expose writable structured and nested aside fields in both modes while excluding readonly declarations",async()=>{
 const fields:Api.FieldInfo[]=[
  {name:"name",title:"Name",type:"text",required:true},
  {name:"queries",title:"Queries",type:"json",aside:true},
  {name:"rules",title:"Rules",type:"lines",fields:[{name:"title",title:"Rule title",type:"text",aside:true},{name:"configuration",title:"Rule configuration",type:"json",aside:true}]},
  {name:"published",title:"Published configuration",type:"json",readOnly:true,aside:true},
 ];
 const info:Api.EntityInfo={app:"build",type:"build.example",title:"Example",plural:"Examples",display:"name",standard:[],fields};
 const initial={name:"Draft",queries:{recent:{limit:0,enabled:false,empty:null},order:["b","a"]},rules:[{title:"Nested rule",configuration:{priority:0}}],published:{private:"not writable"}};
 const original=structuredClone(initial),submit=vi.fn(()=>false);
 render(<RecordForm entity={entityFrom(info)} sourceFields={fields.filter(field=>!field.readOnly)} defaultValues={initial} onSubmit={submit}/>);
 expect(screen.getByRole("combobox",{name:"Queries type"})).toBeTruthy();
 expect(screen.getByDisplayValue("Nested rule")).toBeTruthy();
 expect(screen.getByRole("combobox",{name:"Rule configuration type"})).toBeTruthy();
 expect(screen.queryByRole("combobox",{name:"Published configuration type"})).toBeNull();
 expect(submit).not.toHaveBeenCalled();
 fireEvent.change(screen.getByLabelText("Queries.recent.limit"),{target:{value:"2"}});
 fireEvent.click(screen.getByRole("button",{name:"Source editor"}));
 const source=screen.getByRole("textbox",{name:"Configuration source"}) as HTMLTextAreaElement;
 const next={name:"Draft",queries:{recent:{limit:2,enabled:false,empty:null},order:["b","a"]},rules:initial.rules};
 expect(JSON.parse(source.value)).toEqual(next);
 const changed={...next,queries:{...next.queries,recent:{...next.queries.recent,limit:4}}};
 fireEvent.change(source,{target:{value:JSON.stringify(changed)}});
 await act(async()=>{fireEvent.click(screen.getByRole("button",{name:"Apply source"}));});
 fireEvent.click(screen.getByRole("button",{name:"Visual"}));
 expect((screen.getByLabelText("Queries.recent.limit") as HTMLInputElement).value).toBe("4");
 await act(async()=>{fireEvent.click(screen.getByRole("button",{name:"Save"}));});
 expect(submit).toHaveBeenCalledExactlyOnceWith(changed);
 expect(initial).toEqual(original);
});

test("a structured form distinguishes an omitted field from an explicit null across visual source and original submission",async()=>{
 const fields:Api.FieldInfo[]=[{name:"queries",title:"Queries",type:"json",aside:true}];
 const info:Api.EntityInfo={app:"build",type:"build.example",title:"Example",plural:"Examples",display:"id",standard:[],fields};
 const submit=vi.fn((_values:unknown)=>false);
 render(<RecordForm entity={entityFrom(info)} sourceFields={fields} defaultValues={{queries:{limit:5}}} onSubmit={submit}/>);
 fireEvent.change(screen.getByRole("combobox",{name:"Queries type"}),{target:{value:"unset"}});
 fireEvent.click(screen.getByRole("button",{name:"Source editor"}));
 expect(JSON.parse((screen.getByRole("textbox",{name:"Configuration source"}) as HTMLTextAreaElement).value)).toEqual({});
 await act(async()=>{fireEvent.click(screen.getByRole("button",{name:"Save"}));});
 expect(submit).toHaveBeenCalledTimes(1);
 expect(JSON.parse(JSON.stringify(submit.mock.calls[0]![0]))).toEqual({});
 fireEvent.click(screen.getByRole("button",{name:"Visual"}));
 expect((screen.getByRole("combobox",{name:"Queries type"}) as HTMLSelectElement).value).toBe("unset");
 fireEvent.change(screen.getByRole("combobox",{name:"Queries type"}),{target:{value:"null"}});
 fireEvent.click(screen.getByRole("button",{name:"Source editor"}));
 expect(JSON.parse((screen.getByRole("textbox",{name:"Configuration source"}) as HTMLTextAreaElement).value)).toEqual({queries:null});
 await act(async()=>{fireEvent.click(screen.getByRole("button",{name:"Save"}));});
 expect(submit).toHaveBeenCalledTimes(2);
 expect(submit).toHaveBeenLastCalledWith({queries:null});
});
