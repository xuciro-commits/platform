import { expect, signIn, test, unique } from "./kit";
import type { ReleaseAsset, ReleasePreview, SavedReleaseReview } from "../packages/api/src/gen/host";

test.use({ actionTimeout: 15_000 });

test("F8 a publisher reviews a product version, releases it, and the version is immutable with passing checks required", async ({ page }) => {
  test.setTimeout(240_000);
  const name = unique("tickets");
  const objectName = `${name}ticket`;
  const type = `build.${objectName}`;
  const project = unique("ticketsproject");
  const headers = { authorization: "Bearer builder" };
  let objectID = "";

  const read = async <T,>(path: string): Promise<T> => {
    const response = await page.request.get(path, { headers });
    expect(response.ok(), await response.text()).toBeTruthy();
    return response.json();
  };

  const submit = async (target: { type: string; id: string }, action: string, payload: unknown, expectedRevision: number) => {
    const identity = await read<{ tenantId: string; principalId: string }>("/v1/me");
    const response = await page.request.post("/v1/submissions", {
      headers,
      data: {
        tenantId: identity.tenantId, principalId: identity.principalId, authority: "build", target,
        schema: { name: `${target.type}.${action}`, version: 1 }, idempotencyKey: unique("submit"),
        payload: Buffer.from(JSON.stringify(payload)).toString("base64"), expectedRevision,
      },
    });
    const answer = await response.json();
    expect(response.ok(), JSON.stringify(answer)).toBeTruthy();
    expect(answer.error).toBeUndefined();
    expect(answer.record?.changeId).toEqual(expect.any(String));
    expect(answer.record.changeId).not.toBe("");
    return answer.record as { changeId: string };
  };

  const objectRecord = () => read<{ record: { revision: number; published?: string } }>(`/v1/records/build.object/${objectID}`);

  await test.step("setup: a builder defines a publishable object type with actions and leaves it unpublished", async () => {
    await signIn(page, "builder", "/projects");
    await submit({ type: "build.app", id: project }, "create", { name: project, title: "Ticket project" }, 0);
    objectID = unique("OBJ");
    await submit({ type: "build.object", id: objectID }, "create", {
      name: objectName, title: "Ticket", plural: "Tickets", display: "subject",
      project,
      fields: [
        { name: "subject", title: "Subject", type: "text", required: true, search: true },
        { name: "note", title: "Resolution note", type: "text" },
        { name: "priority", title: "Priority", type: "choice", choices: "low,high" },
      ],
      states: [{ name: "open", title: "Open" }],
    }, 0);
    await expect.poll(async () => (await objectRecord()).record.revision).toBe(1);
    const installed = await objectRecord();
    expect(installed.record.published ?? "", "the object is an unpublished draft so F8 exercises the publish action itself").toBe("");
    const working = await read<{ drafts: { kind: string; id: string }[] }>("/v1/releases/drafts/working");
    expect(working.drafts.some(draft => draft.kind === "object" && draft.id === objectID)).toBe(true);
    await submit({ type: "build.actiontype", id: `${type}.resolve` }, "create", {
      name: `${type}.resolve`, object: type, project,
      definition: { name: "resolve", title: "Resolve ticket", description: "Record how the ticket was resolved.", from: ["open"], to: "",
        inputs: [{ name: "note", title: "Resolution note", type: "text", required: true }], sets: [{ field: "note", from: "note" }] },
    }, 0);
    await submit({ type: "build.actiontype", id: `${type}.escalate` }, "create", {
      name: `${type}.escalate`, object: type, project,
      definition: { name: "escalate", title: "Escalate ticket", description: "Escalate a high-priority ticket.", from: ["open"], to: "",
        conditions: [{ field: "priority", operator: "=", value: "high", message: "Only high-priority tickets can be escalated." }],
        inputs: [{ name: "reason", title: "Escalation reason", type: "text", required: true }], sets: [{ field: "note", from: "reason" }] },
    }, 0);
  });

  await test.step("1 the publisher opens DevOps Releases and starts a new candidate for the draft", async () => {
    await signIn(page, "builder", "/release-review");
    await expect(page.getByRole("heading", { name: "Releases", level: 1 })).toBeVisible({ timeout: 30_000 });
    await expect(page.getByRole("button", { name: "New candidate", exact: true })).toBeVisible();
    await page.goto(`/#/release-review?kind=object&id=${objectID}`);
    await expect(page.getByRole("heading", { name: "New candidate", level: 2 })).toBeVisible();
    await expect(page.getByRole("button", { name: "Check draft and dependencies", exact: true })).toBeEnabled();
  });

  await test.step("2 the release review lists the contents to publish and shows its checks passing", async () => {
    const previewResponse = page.waitForResponse(response => response.url().endsWith("/v1/releases/preview") && response.request().method() === "POST"
      && response.request().postDataJSON().drafts.some((draft: { id: string }) => draft.id === objectID));
    await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
    const preview = await previewResponse;
    expect(preview.ok(), await preview.text()).toBeTruthy();
    const review = await preview.json() as ReleasePreview;
    expect(review.diagnostic, "an object draft with resolvable dependencies passes its checks").toBeUndefined();
    const objectDraft = review.draftReviews!.find(draft => draft.kind === "object" && draft.id === objectID)!;
    expect(objectDraft, "the review lists the object being published").toBeDefined();
    expect(objectDraft.checked, "the object's checks ran").toBe(true);
    expect(objectDraft.issues, "the object has no check problems").toEqual([]);
    expect(review.included.some(ref => ref.app === "build" && ref.kind === "object" && ref.name === `build.${objectName}`), "the contents being published include the object").toBe(true);

    await expect(page.getByText("Candidate ready for review", { exact: true })).toBeVisible();
    const provenance = page.getByRole("group", { name: "Draft provenance" });
    await expect(provenance.getByText("Object types · Ticket").first()).toBeVisible();
    await expect(provenance.getByText("0 issues", { exact: true }).first()).toBeVisible();
    await expect(page.getByRole("heading", { name: /^Added ·/ })).toBeVisible();
    await expect(page.getByRole("heading", { name: /^Included assets ·/ })).toBeVisible();
    await expect(page.getByText(`build/object/build.${objectName}`, { exact: true }).first()).toBeVisible();
  });

  let candidateID = "";
  await test.step("3 the publisher saves the immutable candidate and activates the release", async () => {
    const savedResponse = page.waitForResponse(response => response.url().endsWith("/v1/releases/candidates") && response.request().method() === "POST");
    await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
    const saved = await savedResponse;
    expect(saved.ok(), await saved.text()).toBeTruthy();
    candidateID = (await saved.json()).id as string;
    expect(candidateID).not.toBe("");
    await page.getByRole("button", { name: "Activate release", exact: true }).click();
    await expect(page.getByText("Release active for operators.")).toBeVisible({ timeout: 60_000 });
    await expect.poll(async () => (await objectRecord()).record.published, { timeout: 30_000 }).not.toBe("");
  });

  await test.step("4 success standard: the released version is immutable even as the draft keeps changing", async () => {
    const sealedBodyOf = (review: SavedReleaseReview): ReleaseAsset => {
      const asset = review.assets.find(asset => asset.ref.app === "build" && asset.ref.kind === "object" && asset.ref.name === `build.${objectName}`)!;
      expect(asset, "the sealed candidate carries the object definition").toBeDefined();
      return asset;
    };
    const before = await read<SavedReleaseReview>(`/v1/releases/candidates/${encodeURIComponent(candidateID)}`);
    expect(before.preview.candidateId).toBe(candidateID);
    const sealedBefore = sealedBodyOf(before);
    const sealedSource = sealedBefore.sourceVersion;
    expect(sealedSource).not.toBe("");

    // The UI never offers to edit a saved candidate: its review is read-only and only a fresh draft can be saved.
    await expect(page.getByRole("button", { name: "Save immutable candidate", exact: true })).toHaveCount(0);
    await expect(page.getByText("Saved candidate review", { exact: true })).toBeVisible();

    // Changing the underlying draft after the release must not alter the sealed version's bytes.
    const current = await objectRecord();
    await submit({ type: "build.object", id: objectID }, "edit", {
      fields: [
        { name: "subject", title: "Subject", type: "text", required: true, search: true },
        { name: "note", title: "Resolution note", type: "text" },
        { name: "priority", title: "Priority", type: "choice", choices: "low,high" },
        { name: "owner", title: "Owner", type: "text" },
      ],
    }, current.record.revision);
    const after = await read<SavedReleaseReview>(`/v1/releases/candidates/${encodeURIComponent(candidateID)}`);
    const sealedAfter = sealedBodyOf(after);
    expect(after.preview.candidateId).toBe(candidateID);
    expect(sealedAfter.sourceVersion, "the sealed version pin is unchanged").toBe(sealedSource);
    expect(JSON.stringify(sealedAfter.body), "the sealed definition bytes are unchanged").toBe(JSON.stringify(sealedBefore.body));
    const owner = (JSON.stringify(sealedAfter.body) as string).includes("\"owner\"");
    expect(owner, "the field added to the draft is absent from the sealed candidate").toBe(false);
  });

  await test.step("5 success standard: a candidate that fails its checks cannot be saved for release", async () => {
    const badPageID = unique("PAGE");
    const badPageName = `${name}BadPage`;
    const created: { type: string; id: string }[] = [];
    try {
      await submit({ type: "build.page", id: badPageID }, "create", {
        name: badPageName, title: "Release gate page", project, object: type, list: ["subject"], detail: ["subject"],
      }, 0);
      created.push({ type: "build.page", id: badPageID });

      await page.goto(`/#/release-review?kind=page&id=${badPageID}`);
      await expect(page.getByRole("button", { name: "Check draft and dependencies", exact: true })).toBeEnabled();
      const previewResponse = page.waitForResponse(response => response.url().endsWith("/v1/releases/preview") && response.request().method() === "POST"
        && response.request().postDataJSON().drafts.some((draft: { id: string }) => draft.id === badPageID));
      await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
      const preview = await previewResponse;
      expect(preview.ok(), await preview.text()).toBeTruthy();
      const review = await preview.json() as ReleasePreview;
      expect(review.diagnostic, "a failing check rejects the candidate").toBeTruthy();
      const pageReview = review.draftReviews!.find(draft => draft.kind === "page" && draft.id === badPageID)!;
      expect(pageReview).toBeDefined();
      expect(pageReview.checked).toBe(true);
      expect(pageReview.issues.length, "the page lists its check problems").toBeGreaterThan(0);
      expect(pageReview.issues.join("\n")).toContain(`the name "${badPageName}" is not lower-case letters and digits`);

      await expect(page.getByText("Candidate rejected", { exact: true })).toBeVisible();
      const provenance = page.getByRole("group", { name: "Draft provenance" });
      const row = provenance.getByRole("treeitem").filter({ hasText: "Release gate page" }).last();
      await expect(row.getByText(`${pageReview.issues.length} issues`, { exact: true })).toBeVisible();
      await row.click();
      const selected = page.getByRole("region", { name: "Selected change" });
      const issues = selected.getByRole("list", { name: `${pageReview.issues.length} issues`, exact: true });
      await expect(issues.getByRole("listitem").first()).toContainText("is not lower-case letters and digits");
      // Checks must pass before publishing: the save/release control is withheld entirely.
      await expect(page.getByRole("button", { name: "Save immutable candidate", exact: true })).toHaveCount(0);
    } finally {
      for (const draft of created.reverse()) {
        const current = await read<{ record: { revision: number } }>(`/v1/records/${draft.type}/${draft.id}`);
        await submit({ type: draft.type, id: draft.id }, "archive", {}, current.record.revision);
      }
    }
  });

  await test.step("cleanup: archive the draft this case created when the platform still allows it", async () => {
    // A published object type keeps its installed definition and cannot be archived; the acceptance host is
    // isolated and discarded per run, so the released object leaves no shared state behind.
    const current = await objectRecord();
    const identity = await read<{ tenantId: string; principalId: string }>("/v1/me");
    const response = await page.request.post("/v1/submissions", {
      headers,
      data: {
        tenantId: identity.tenantId, principalId: identity.principalId, authority: "build",
        target: { type: "build.object", id: objectID }, schema: { name: "build.object.archive", version: 1 },
        idempotencyKey: unique("submit"), payload: Buffer.from(JSON.stringify({})).toString("base64"), expectedRevision: current.record.revision,
      },
    });
    if (response.ok()) {
      await expect.poll(async () => (await read<{ record: { archived?: boolean } }>(`/v1/records/build.object/${objectID}`)).record.archived).toBe(true);
    } else {
      expect(JSON.stringify(await response.json())).toContain("published object cannot be archived");
    }
  });
});
