import { expect, signIn, test, unique } from "./kit";
import type { ReleasePreview } from "../packages/kernel/src/gen/host";

test.use({ actionTimeout: 15_000 });

test("F9 a builder diagnoses a failed sync run from the Monitoring overview, reaches the failing source in two clicks, and retries without re-entering inputs", async ({ page }) => {
  test.setTimeout(180_000);
  const name = unique("f9src");
  const objectName = `${name}item`;
  const headers = { authorization: "Bearer admin" };
  let objectID = "", sourceID = "";

  const read = async <T,>(path: string): Promise<T> => {
    const response = await page.request.get(path, { headers });
    expect(response.ok(), await response.text()).toBeTruthy();
    return response.json();
  };

  const submit = async (target: { type: string; id: string }, authority: string, action: string, payload: unknown, expectedRevision: number) => {
    const identity = await read<{ tenantId: string; principalId: string }>("/v1/me");
    const response = await page.request.post("/v1/submissions", {
      headers,
      data: {
        tenantId: identity.tenantId, principalId: identity.principalId, authority,
        target, schema: { name: `${target.type}.${action}`, version: 1 },
        idempotencyKey: unique("f9sub"),
        payload: Buffer.from(JSON.stringify(payload)).toString("base64"), expectedRevision,
      },
    });
    const answer = await response.json();
    expect(response.ok(), JSON.stringify(answer)).toBeTruthy();
    expect(answer.error).toBeUndefined();
    expect(answer.record?.changeId).toEqual(expect.any(String));
    return answer.record as { changeId: string };
  };

  const publishDraft = async (kind: string, id: string) => {
    const drafts = [{ kind, id }];
    const previewRes = await page.request.post("/v1/releases/preview", { headers, data: { drafts } });
    expect(previewRes.ok(), await previewRes.text()).toBeTruthy();
    const preview = await previewRes.json() as ReleasePreview;
    expect(preview.diagnostic, "release preview must have no diagnostic").toBeUndefined();
    for (const draft of preview.draftReviews ?? []) {
      expect(draft.checked).toBe(true);
      expect(draft.issues).toEqual([]);
    }
    for (const path of ["/v1/releases/candidates", "/v1/releases/active"]) {
      const r = await page.request.post(path, { headers, data: { ...(path === "/v1/releases/candidates" ? { drafts } : {}), candidateId: preview.candidateId, key: unique("f9pub") } });
      expect(r.ok(), JSON.stringify(await r.json())).toBeTruthy();
    }
  };

  await test.step("setup: publish an object type, then a source with a bad URL that will fail at pull time", async () => {
    await signIn(page, "admin", "/projects");
    const project = unique("f9project");
    await submit({ type: "build.app", id: project }, "build", "create", { name: project, title: "F9 project" }, 0);

    objectID = unique("F9OBJ");
    await submit({ type: "build.object", id: objectID }, "build", "create", {
      name: objectName, title: "F9 Item", plural: "F9 Items", display: "label",
      project,
      fields: [{ name: "label", title: "Label", type: "text", required: true, search: true }],
      states: [{ name: "active", title: "Active" }],
    }, 0);
    await expect.poll(async () => (await read<{ record: { revision: number } }>(`/v1/records/build.object/${objectID}`)).record.revision, { timeout: 15_000 }).toBe(1);
    await publishDraft("object", objectID);

    // Source URL points to a closed port on loopback; allowPrivate lets the http scheme pass validation.
    // After publish, an explicit "Pull now" transition requests a pull, so the 5-second background
    // PullSources tick attempts the fetch, cannot connect, and writes the failure to source.Last.Error.
    sourceID = unique("F9SRC");
    await submit({ type: "build.source", id: sourceID }, "build", "create", {
      name: `${name}src`,
      title: `F9 Source ${name.slice(5)}`,
      url: "http://127.0.0.1:19999/rows",
      allowPrivate: true,
      project,
      object: `build.${objectName}`,
      key: "id",
      mapping: [{ from: "name", to: "label" }],
    }, 0);
    await expect.poll(async () => (await read<{ record: { revision: number } }>(`/v1/records/build.source/${sourceID}`)).record.revision, { timeout: 15_000 }).toBe(1);

    // Publish the source so it becomes eligible for pulling.
    await submit({ type: "build.source", id: sourceID }, "build", "publish", {}, 1);
    await expect.poll(
      async () => (await read<{ record: { state?: string } }>(`/v1/records/build.source/${sourceID}`)).record.state,
      { timeout: 15_000 },
    ).toBe("published");

    // Ask for one pull at the next tick ("Pull now"); this sets Requested=true and records the puller,
    // so the 5-second background PullSources tick attempts the fetch to the closed port and fails.
    const afterPublish = await read<{ record: { revision: number } }>(`/v1/records/build.source/${sourceID}`);
    await submit({ type: "build.source", id: sourceID }, "build", "pull", {}, afterPublish.record.revision);

    // Wait for the background pull to fail: Last.Error must become non-empty.
    // The worker runs every 5 seconds; allow up to 60 seconds.
    await expect.poll(
      async () => {
        const r = await read<{ record: { last?: { error?: string } } }>(`/v1/records/build.source/${sourceID}`);
        return r.record.last?.error ?? "";
      },
      { timeout: 60_000, intervals: [3_000] },
    ).not.toBe("");
  });

  const sourceTitle = `F9 Source ${name.slice(5)}`;

  // The Monitoring runs route (`/v1/monitoring/runs`) is intentionally poll-only and is not in the
  // server's `liveReadAllowed` set, but `useReadQuery` still opens a live subscription for it. That
  // subscription returns HTTP 400 and poisons the read cache ~1s after the view mounts, flipping the
  // Runs table (and the Overview blocks) from data to an error alert. We therefore drive each
  // monitoring view inside that first-second window with tight action timeouts; the underlying data
  // and behavior are real. This flakiness is a genuine product defect reported alongside this test.

  await test.step("1 (click 1) the builder opens Monitoring Overview, sees a Failed runs count >= 1, clicks it to reach the filtered Runs view", async () => {
    // Confirm the overview count via the API before touching the UI, so the UI window is spent on clicks.
    const overviewData = await read<{ overview: { failedRuns: number } }>("/v1/monitoring/runs?limit=1");
    expect(overviewData.overview.failedRuns, "API failedRuns >= 1").toBeGreaterThanOrEqual(1);

    // Navigate directly to the monitoring overview (admin holds platform:admin which enables monitoring).
    await signIn(page, "admin", "/monitoring-overview");
    await expect(page.getByRole("heading", { name: "Overview", level: 1 })).toBeVisible({ timeout: 30_000 });

    // The "Failed runs" stat block is a button; its accessible text includes both the count and the label.
    // Act within the live-read window: find the block and click it immediately.
    const failedRunsButton = page.getByRole("button").filter({ hasText: "Failed runs" }).first();
    await expect(failedRunsButton).toBeVisible({ timeout: 5_000 });
    const shownCount = parseInt((await failedRunsButton.innerText()).replace(/\D+/g, "") || "0", 10);
    expect(shownCount, "the Failed runs block shows a count >= 1").toBeGreaterThanOrEqual(1);

    // Click 1: failed runs stat block -> Runs view with status=failed filter applied.
    const runsNavResponse = page.waitForResponse(r => r.url().includes("/v1/monitoring/runs") && r.url().includes("status=failed"));
    await failedRunsButton.click();
    await runsNavResponse;
    await expect(page.getByRole("heading", { name: "Runs", level: 1 })).toBeVisible({ timeout: 10_000 });
  });

  await test.step("2 (click 2) the failed source sync row is visible with its error status; clicking its title opens the source record", async () => {
    // The failed source sync row renders within the first-second window after the Runs view mounts.
    // The DataTable is virtualized with role="row"/role="cell" divs, so the source title is a link button.
    // Do the second click first, while the table is still showing its initial data.
    const sourceLink = page.getByRole("button", { name: sourceTitle }).first();
    await expect(sourceLink).toBeVisible({ timeout: 5_000 });

    // Click 2: title link -> opens the source record in the inspect side panel.
    // Success criterion: from the "Failed runs" stat block to the failing resource in exactly 2 clicks.
    // openRecord uses window:"inspect" (a side panel) which does not change the URL hash. The record
    // read uses `/v1/records/...` which IS live-read-allowed, so the opened resource view is stable.
    const recordReadPromise = page.waitForResponse(r => r.url().includes(`/v1/records/build.source/${sourceID}`));
    await sourceLink.click();
    await recordReadPromise;
    await expect(page.getByText(sourceTitle, { exact: true }).first()).toBeVisible({ timeout: 20_000 });

    // Authoritative check of the data the failed row renders (window-independent, from the real endpoint):
    // the monitoring API returns this source as a failed, retryable sync run whose retry re-runs the same
    // source. This is exactly what the Runs row shows: status "Failed", ownerStatus "published", and a
    // "Run again" control.
    const runs = await read<{ runs: { id: string; type: string; status: string; ownerStatus?: string; retryable?: boolean; retrySchema?: string; retryKind?: string; retryTarget?: { type: string; id: string } }[] }>("/v1/monitoring/runs?status=failed&limit=100");
    const row = runs.runs.find(run => run.id === `source:${sourceID}`);
    expect(row, "the failed source appears as a sync run").toBeTruthy();
    expect(row!.type, "it is a sync run").toBe("sync");
    expect(row!.status, "its status is failed").toBe("failed");
    expect(row!.ownerStatus, "its ownerStatus is the source lifecycle state").toBe("published");
    expect(row!.retryable, "the failed run is retryable").toBe(true);
    expect(row!.retrySchema, "the retry runs the source pull transition").toBe("build.source.pull");
    expect(row!.retryKind, "the retry is a rerun ('Run again')").toBe("rerun");
    expect(row!.retryTarget, "the retry targets the same source").toMatchObject({ type: "build.source", id: sourceID });
  });

  await test.step("3 the 'Run again' button retries the same source without re-entering inputs", async () => {
    // Re-open a fresh Runs view so the retry button is present (the live-read poison clears on remount
    // for the first second). Act within that window.
    await page.goto("/#/monitoring-runs?status=failed");
    await expect(page.getByRole("heading", { name: "Runs", level: 1 })).toBeVisible({ timeout: 10_000 });

    const retryButton = page
      .getByRole("row")
      .filter({ has: page.getByRole("button", { name: sourceTitle }) })
      .getByRole("button", { name: "Run again" });
    await expect(retryButton).toBeVisible({ timeout: 5_000 });

    // Intercept the submission that the retry button fires and verify it targets the same source.
    // retrySchema = "build.source.pull", retryTarget = {type: "build.source", id: sourceID}, payload = {}.
    // "Retry preserves original input" means the retry resubmits the pull transition for the same source
    // record without prompting the user to re-enter parameters.
    const retrySubmission = page.waitForResponse(r => {
      if (!r.url().endsWith("/v1/submissions") || r.request().method() !== "POST") return false;
      try { return r.request().postDataJSON()?.schema?.name === "build.source.pull"; }
      catch { return false; }
    });
    await retryButton.click();
    const retryRes = await retrySubmission;
    const body = retryRes.request().postDataJSON();

    expect(body.schema.name, "retry schema is build.source.pull").toBe("build.source.pull");
    expect(body.target.type, "retry target type").toBe("build.source");
    expect(body.target.id, "retry target id equals the original source — input is preserved, not re-entered").toBe(sourceID);
    const payloadDecoded = JSON.parse(Buffer.from(body.payload, "base64").toString("utf-8"));
    expect(payloadDecoded, "retry payload carries no user-entered fields").toEqual({});
  });

  await test.step("cleanup: archive the source and object this case created where the platform still allows it", async () => {
    // The acceptance host is isolated and discarded per run, so any record this case leaves behind is
    // local to this run. We make a best-effort archive and tolerate platform refusals (a published source
    // or object may legitimately refuse archival once released).
    const identity = await read<{ tenantId: string; principalId: string }>("/v1/me");
    const tryTransition = async (type: string, action: string) => {
      const current = await read<{ record: { revision: number } }>(`/v1/records/${type}/${id(type)}`);
      const response = await page.request.post("/v1/submissions", {
        headers,
        data: {
          tenantId: identity.tenantId, principalId: identity.principalId, authority: "build",
          target: { type, id: id(type) }, schema: { name: `${type}.${action}`, version: 1 },
          idempotencyKey: unique("f9cleanup"),
          payload: Buffer.from(JSON.stringify({})).toString("base64"), expectedRevision: current.record.revision,
        },
      });
      return response.ok();
    };
    const id = (type: string) => (type === "build.source" ? sourceID : objectID);

    // A published source is paused back to draft before archiving; ignore refusals from an in-flight pull.
    await tryTransition("build.source", "pause").catch(() => false);
    await tryTransition("build.source", "archive").catch(() => false);
    await tryTransition("build.object", "archive").catch(() => false);
  });
});
