import { readFile } from "node:fs/promises";
import { acceptedClient, expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 15_000 });

test("F8 a released media set supports object attachments and retains their bytes after removal", async ({ page }) => {
  test.setTimeout(180_000);
  const { headers, read, submit, publish } = acceptedClient(page);
  const name = unique("documents"), object = unique("OBJECT"), record = unique("RECORD");
  const project = unique("documentsproject");
  const type = `build.${name}document`;
  const title = `Media ${name}`, filename = "retained-document.txt";
  const bytes = Buffer.from(`Accepted attachment ${name}\nOriginal bytes remain readable.\n`);
  let mediaSet = "", hash = "";
  await submit("build.app", project, "create", { name: project, title: "Document project" });

  await test.step("a publisher creates a media set and uploads the original file through Dataset Preview", async () => {
    await signIn(page, "admin", `/media-set?id=new&application=${encodeURIComponent(project)}`);
    await page.getByRole("textbox", { name: "Name", exact: true }).fill(name);
    await page.getByRole("textbox", { name: "Title", exact: true }).fill(title);
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect.poll(async () => {
      const list = await read<{ records: { id: string; name: string }[] }>("/v1/records/build.mediaset?limit=200");
      mediaSet = list.records.find(row => row.name === name)?.id ?? "";
      return mediaSet;
    }).not.toBe("");
    await expect(page.getByRole("button", { name: "Upload file", exact: true })).toBeVisible();
    await page.locator('input[type="file"]').setInputFiles({ name: filename, mimeType: "text/plain", buffer: bytes });
    await expect.poll(async () => {
      const set = await read<{ record: { version: number; items: { hash: string }[] } }>(`/v1/records/build.mediaset/${mediaSet}`);
      hash = set.record.items[0]?.hash ?? "";
      return set.record.version;
    }).toBe(1);
    expect(hash).toMatch(/^[a-f0-9]{64}$/);
    await expect(page.getByRole("cell", { name: filename, exact: true })).toBeVisible();
  });

  await submit("build.object", object, "create", {
    name: `${name}document`, title: "Document", plural: "Documents", display: "subject", project,
    fields: [{ name: "subject", title: "Subject", type: "text", required: true }, { name: "document", title: "Document", type: "attachment" }],
    states: [{ name: "open", title: "Open" }],
  });
  await submit("build.actiontype", `${type}.attach`, "create", {
    name: `${type}.attach`, object: type, project,
    definition: { name: "attach", title: "Attach document", from: ["open"], to: "", inputs: [{ name: "document", title: "Document", type: "attachment", required: true }], sets: [{ field: "document", from: "document" }] },
  });
  const candidate = await publish([{ kind: "mediaset", id: mediaSet }, { kind: "object", id: object }]);
  await submit(type, record, "create", { subject: `Document ${name}` });

  await test.step("the installed object's typed action selects a media item and writes only its reference", async () => {
    await page.goto(`/#/record?type=${encodeURIComponent(type)}&id=${record}`);
    await page.getByRole("button", { name: "Attach document", exact: true }).click();
    const form = page.getByRole("dialog", { name: `Attach document ${record}`, exact: true });
    await form.getByRole("combobox", { name: "Media set", exact: true }).fill(title);
    await form.getByRole("option", { name: `${mediaSet} · ${title}`, exact: true }).click();
    await form.getByRole("combobox", { name: "Media item", exact: true }).selectOption(hash);
    await form.getByRole("button", { name: "Attach document", exact: true }).click();
    await expect(form).toBeHidden();
    await expect.poll(async () => (await read<{ record: { document?: { mediaSet: string; item: string } } }>(`/v1/records/${type}/${record}`)).record.document).toEqual({ mediaSet, item: hash });
  });

  await test.step("removing the current item retains both the historical download and existing attachment", async () => {
    await page.goto(`/#/media-set?id=${mediaSet}`);
    await page.getByRole("button", { name: "Remove", exact: true }).click();
    await expect.poll(async () => (await read<{ record: { version: number; items: unknown[] } }>(`/v1/records/build.mediaset/${mediaSet}`)).record).toMatchObject({ version: 2, items: [] });
    const retained = await read<{ record: { items: { hash: string }[] } }>(`/v1/records/build.mediasetversion/${encodeURIComponent(`${mediaSet}@1`)}`);
    expect(retained.record.items.map(item => item.hash)).toEqual([hash]);
    for (const suffix of ["", "?version=1"]) {
      const response = await page.request.get(`/v1/media-sets/${mediaSet}/items/${hash}${suffix}`, { headers });
      expect(response.ok(), await response.text()).toBeTruthy();
      expect(await response.body()).toEqual(bytes);
    }
    await page.getByRole("combobox", { name: "Version", exact: true }).selectOption("1");
    const downloading = page.waitForEvent("download");
    await page.getByRole("button", { name: "Download", exact: true }).click();
    const download = await downloading;
    expect(download.suggestedFilename()).toBe(filename);
    expect(await readFile((await download.path())!)).toEqual(bytes);
    expect((await read<{ record: { document: unknown } }>(`/v1/records/${type}/${record}`)).record.document).toEqual({ mediaSet, item: hash });
    await page.goto(`/#/record?type=${encodeURIComponent(type)}&id=${record}`);
    const attachmentDownload = page.waitForEvent("download");
    await page.getByRole("button", { name: filename, exact: true }).click();
    expect(await readFile((await (await attachmentDownload).path())!)).toEqual(bytes);
  });

  await test.step("immutable product contents retain original bytes and media history through the installed upgrade", async () => {
    await page.goto(`/#/release-review?kind=mediaset&id=${mediaSet}`);
    await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
    await expect(page.getByText("Candidate ready for review", { exact: true })).toBeVisible();
    const saving = page.waitForResponse(response => response.url().endsWith("/v1/releases/candidates") && response.request().method() === "POST");
    await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
    const saved = await saving;
    expect(saved.ok(), await saved.text()).toBeTruthy();
    const upgraded = (await saved.json()).id as string;
    await page.getByRole("button", { name: "Activate release", exact: true }).click();
    await expect(page.getByText("Release active for operators.")).toBeVisible({ timeout: 60_000 });
    type Saved = { assets: { ref: { kind: string; name: string }; body: { version: number; items: unknown[]; history: { version: number }[]; blobs: Record<string, string> } }[] };
    const first = await read<Saved>(`/v1/releases/candidates/${encodeURIComponent(candidate)}`);
    const next = await read<Saved>(`/v1/releases/candidates/${encodeURIComponent(upgraded)}`);
    const original = first.assets.find(asset => asset.ref.kind === "mediaset")!.body;
    const installed = next.assets.find(asset => asset.ref.kind === "mediaset")!.body;
    expect(original.version).toBe(1);
    expect(original.items).toHaveLength(1);
    expect(installed.version).toBe(2);
    expect(installed.items).toEqual([]);
    expect(installed.history.map(version => version.version)).toContain(1);
    expect(Buffer.from(installed.blobs[hash], "base64")).toEqual(bytes);
    expect(installed.blobs[hash]).toBe(original.blobs[hash]);
    expect((await read<{ record: { version: number; items: unknown[] } }>(`/v1/records/build.mediaset/${mediaSet}`)).record).toMatchObject({ version: 2, items: [] });
    const installedDownload = await page.request.get(`/v1/media-sets/${mediaSet}/items/${hash}?version=1`, { headers });
    expect(installedDownload.ok(), await installedDownload.text()).toBeTruthy();
    expect(await installedDownload.body()).toEqual(bytes);
  });

  await submit(type, record, "archive", {});
});
