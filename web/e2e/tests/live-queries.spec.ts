import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("two workspaces receive relevant lists and pivots without browser refetches or unrelated business reads", async ({ page, context, request }) => {
 test.setTimeout(90000);
 const name = fresh("live").replace(/[^a-z0-9]/gi, "").toLowerCase();
 const types = [`build.${name}one`, `build.${name}two`];
 for (const type of types) {
  const object = fresh("OBJ");
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: object }, { name: type.slice(6), title: type, fields: [{ name: "note", title: "Note", type: "text", search: true }, { name: "bucket", title: "Bucket", type: "choice", choices: "ready" }] });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: object }, {});
  await decide(request, "manager", "build", `${type}.create`, { type, id: "FIRST" }, { note: "Original live record" });
 }
 const streamRegistrations: string[]=[];
 page.on("request", r=>{if(r.url().includes("/v1/changes?"))streamRegistrations.push(decodeURIComponent(r.url()));});
 const other = await context.newPage();
 for (const [view, type] of [[page, types[0]!], [other, types[1]!]] as const) {
  await open(view, "manager", "/records");
  await view.getByLabel("Entity type", { exact: true }).selectOption(type);
  await expect(view.getByText("Original live record", { exact: true })).toBeVisible();
 }
 // Observe the real transport after the initial snapshots have settled.
 await expect.poll(()=>streamRegistrations.some(url=>url.includes(types[0]!))).toBe(true);
 const reads: string[] = [];
 page.on("request", r => { if (new URL(r.url()).pathname === `/v1/records/${types[0]}`) reads.push(r.url()); });
 await decide(request, "manager", "build", `${types[1]}.create`, { type: types[1]!, id: "OTHER" }, { note: "Other window update" });
 await expect(other.getByText("Other window update", { exact: true })).toBeVisible();
 expect(reads).toEqual([]);
 await decide(request, "manager", "build", `${types[0]}.create`, { type: types[0]!, id: "SECOND" }, { note: "Pushed live record" });
 await expect(page.getByText("Pushed live record", { exact: true })).toBeVisible();
 expect(reads).toEqual([]);
 await page.getByRole("button", { name: "pivot", exact: true }).click();
 await expect(page.getByRole("table")).toBeVisible();
 const aggregates: string[]=[];
 page.on("request",r=>{if(new URL(r.url()).pathname===`/v1/aggregates/${types[0]}/query`)aggregates.push(r.url());});
 await decide(request, "manager", "build", `${types[0]}.create`, { type: types[0]!, id: "THIRD" }, { note: "Third live record" });
 await expect(page.getByRole("table").getByText("3",{exact:true}).first()).toBeVisible();
 expect(aggregates).toEqual([]);
});
