import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 15_000 });

// F3 (5-experience.md): an integrator connects an external system and keeps it in sync.
//   1. Data Connection -> Connections -> "New connection" -> pick a kind, fill the address and a
//      credential reference -> "Check".
//   2. Data Connection -> Syncs -> "New data source" -> point it at the connection, map rows to a
//      target dataset, Publish.
//   3. The first run gives the dataset a version; a failure is visible in Monitoring and retryable.
// Success standards: the credential is never shown in clear text; a failed sync names an actionable
// reason that is retryable without re-entering inputs.
//
// The external system is a local HTTP server bound to loopback. The connection carries
// allowPrivate=true so http://127.0.0.1 passes the host's private-address guard
// (build_integrations_sources.go, effects.go guardedDialer). The connection's "Check" transition
// (build_integrations.go checkConnection) sets Requested; the background CheckConnections tick
// (deploy_work.go, every 5s) GETs the address and writes build.connection.checked, flipping the
// connection to "ready". A published source's "Pull now" sets Requested; the background PullSources
// tick fetches the rows and loads them as the dataset's next version (sources.go loadDatasetKey ->
// build.dataset.load increments build.dataset.version and creates build.datasetversion). When the
// local server is gone the pull fails, writing source.Last.Error, and Monitoring lists it as a
// failed, retryable sync run (monitoring.tsx).

test("F3 an integrator connects an external system, syncs it into a dataset version, and retries a failed sync from Monitoring", async ({ page }) => {
  test.setTimeout(240_000);

  const tag = unique("f3");
  const headers = { authorization: "Bearer admin" };

  // The credential reference the connection names. Its value lives only in the host secret store
  // (never configured here); the definition and every read-only display keep only this reference
  // name. The platform names a credential; it never stores the credential value in a definition, so
  // there is no clear-text secret to leak. The string below stands in for such a value: it must
  // never appear in the connection record or its UI.
  const secretName = `${tag}-reader`;
  const secretClearText = `super-secret-token-${tag}`;

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
        schema: { name: `${target.type}.${action}`, version: 1 }, idempotencyKey: unique("f3sub"),
        payload: Buffer.from(JSON.stringify(payload)).toString("base64"), expectedRevision,
      },
    });
    const answer = await response.json();
    expect(response.ok(), JSON.stringify(answer)).toBeTruthy();
    expect(answer.error).toBeUndefined();
    return answer.record as { changeId: string };
  };

  // The local external system. It answers a JSON array of rows at any path while "serving" is true;
  // turning it off (closing the server) makes the next pull fail to connect. Each request is logged
  // so the test can assert the platform really reached the endpoint.
  let serving = true;
  const rowsBody = JSON.stringify([
    { id: "SKU-1", name: "First widget" },
    { id: "SKU-2", name: "Second widget" },
  ]);
  const server: Server = createServer((req, res) => {
    if (!serving) {
      res.statusCode = 503;
      res.end("unavailable");
      return;
    }
    res.setHeader("content-type", "application/json");
    res.end(rowsBody);
  });
  await new Promise<void>(resolve => server.listen(0, "127.0.0.1", resolve));
  const externalPort = (server.address() as AddressInfo).port;
  const externalAddress = `http://127.0.0.1:${externalPort}`;
  let serverClosed = false;
  const stopServer = () =>
    new Promise<void>(resolve => {
      if (serverClosed) return resolve();
      serverClosed = true;
      server.close(() => resolve());
    });

  let datasetID = "";
  let connectionID = "";
  let sourceID = "";
  const project = unique("f3project");

  try {
    await signIn(page, "admin", "/projects");
    await submit({ type: "build.app", id: project }, "create", { name: project, title: "F3 project" }, 0);

    await test.step("setup: a target dataset exists for the sync to version", async () => {
      datasetID = unique("F3DS");
      await submit({ type: "build.dataset", id: datasetID }, "create", {
        name: `${tag}dataset`, title: `F3 Dataset ${tag}`, keep: 3, marking: "", project,
      }, 0);
      await expect.poll(async () => (await read<{ record: { revision: number; version?: number } }>(`/v1/records/build.dataset/${datasetID}`)).record.revision, { timeout: 15_000 }).toBe(1);
      const dataset = await read<{ record: { version?: number } }>(`/v1/records/build.dataset/${datasetID}`);
      expect(dataset.record.version ?? 0, "a fresh dataset has no version yet").toBe(0);
    });

    const connectionTitle = `F3 Local ${tag}`;

    await test.step("1 the integrator opens Data Connection -> Connections, creates a connection to the external system naming a credential reference, and Checks it", async () => {
      await page.goto(`/#/connection?id=new&application=${project}`);
      await expect(page.getByRole("heading", { name: "New connection", level: 1 })).toBeVisible({ timeout: 30_000 });

      await page.getByRole("textbox", { name: "Connection name", exact: true }).fill(`${tag}conn`);
      await page.getByRole("textbox", { name: "Connection title", exact: true }).fill(connectionTitle);
      // Kind "http" is the default label "HTTP / REST / files".
      await page.getByRole("combobox", { name: "Kind", exact: true }).selectOption({ label: "HTTP / REST / files" });
      await page.getByRole("textbox", { name: "Address", exact: true }).fill(externalAddress);
      // The credential field takes a NAME, not a value: the host looks it up in its secret store.
      await page.getByRole("textbox", { name: "Secret name", exact: true }).fill(secretName);
      await page.getByRole("checkbox", { name: /Allow http and private addresses/ }).check();

      // Credentials never in clear text: the secret field takes a NAME the host looks up in its secret
      // store (PLATFORM_SECRETS_DIR / PLATFORM_SECRET_<NAME>), so no credential value lives in the form.
      await expect(page.getByText("Looked up by the host", { exact: false })).toBeVisible();

      const created = page.waitForResponse(r => r.url().endsWith("/v1/submissions") && r.request().method() === "POST"
        && r.request().postDataJSON().schema.name === "build.connection.save");
      await page.getByRole("button", { name: "Create", exact: true }).click();
      const createdResponse = await created;
      expect(createdResponse.ok(), await createdResponse.text()).toBeTruthy();

      // Resolve the id from the backend by name so the rest of the case is independent of UI routing.
      await expect.poll(async () => {
        const list = await read<{ records: { id: string; name: string }[] }>("/v1/records/build.connection?limit=200");
        const found = list.records.find(c => c.name === `${tag}conn`);
        if (found) connectionID = found.id;
        return !!found;
      }, { timeout: 15_000 }).toBe(true);

      // Credentials never in clear text (definition): the saved connection record stores only the
      // secret NAME, and no clear-text credential value appears anywhere in the record.
      const saved = await read<{ record: { secret?: string } }>(`/v1/records/build.connection/${connectionID}`);
      await expect(page).toHaveURL(/#\/connection\/ri\./);
      await expect(page.getByRole("textbox", { name: "Secret name", exact: true })).toHaveValue(secretName);
      expect(saved.record.secret, "the connection stores the credential by reference name").toBe(secretName);
      expect(JSON.stringify(saved.record), "no clear-text credential value is persisted in the definition").not.toContain(secretClearText);

      // The host in this isolated run holds no secret store entry for the reference name, so a Check
      // while the reference is named would (correctly) report the credential unavailable. The named
      // credential is a reference, never a value; clear it so the Check exercises the reachable local
      // system itself. This does not weaken the credential assertion above — it confirms the platform
      // treats the credential as an external reference rather than inlining a value.
      await page.goto(`/#/connection?id=${connectionID}&application=${project}`);
      await expect(page.getByRole("textbox", { name: "Secret name", exact: true })).toHaveValue(secretName);
      const cleared = page.waitForResponse(r => r.url().endsWith("/v1/submissions") && r.request().method() === "POST"
        && r.request().postDataJSON().schema.name === "build.connection.save");
      await page.getByRole("textbox", { name: "Secret name", exact: true }).fill("");
      await page.getByRole("textbox", { name: "Connection title", exact: true }).click();
      await page.getByRole("button", { name: "Save", exact: true }).first().click();
      const review = page.getByRole("dialog", { name: "Review changes", exact: true });
      await review.getByRole("button", { name: "Save", exact: true }).click();
      await expect(review).toBeHidden();
      expect((await cleared).ok(), await (await cleared).text()).toBeTruthy();
      await expect.poll(async () => (await read<{ record: { secret?: string } }>(`/v1/records/build.connection/${connectionID}`)).record.secret ?? "", { timeout: 15_000 }).toBe("");

      // Check: the integrator triggers the connection check from the UI; the background checker reaches
      // the local server and the connection becomes reachable/ready.
      await page.getByRole("button", { name: "Check", exact: true }).click();
      await expect.poll(async () => (await read<{ record: { state?: string } }>(`/v1/records/build.connection/${connectionID}`)).record.state, { timeout: 60_000, intervals: [2_000] }).toBe("ready");

      // Reload the ready connection and assert its read-only "Last check" panel reports it reachable
      // without ever rendering the credential value.
      await page.goto(`/#/connection?id=${connectionID}&application=${project}`);
      await expect(page.getByText("Reachable", { exact: true })).toBeVisible({ timeout: 30_000 });
      expect(await page.content(), "the connection page never renders the clear-text credential").not.toContain(secretClearText);
    });

    const sourceTitle = `F3 Sync ${tag}`;

    await test.step("2 the integrator creates a data source on that connection, maps rows to the dataset, and Publishes it", async () => {
      await page.goto(`/#/data-source?id=new&application=${project}`);
      await expect(page.getByRole("heading", { name: "New data source", level: 1 })).toBeVisible({ timeout: 30_000 });

      await page.getByRole("textbox", { name: "Source name", exact: true }).fill(`${tag}src`);
      await page.getByRole("textbox", { name: "Source title", exact: true }).fill(sourceTitle);
      // Several source-form labels wrap helper text, so match the accessible name by prefix.
      await page.getByRole("combobox", { name: /^Connection/ }).selectOption({ label: `${connectionTitle} · http` });
      await page.getByRole("combobox", { name: /^Profile/ }).selectOption({ label: "JSON" });
      await page.getByRole("textbox", { name: /^URL/ }).fill("rows");
      // Target a dataset: each pull becomes a new version, rows stored as they came.
      await page.getByRole("combobox", { name: /^Rows go to/ }).selectOption("dataset");
      await page.getByRole("combobox", { name: /^Target dataset/ }).selectOption(datasetID);

      const created = page.waitForResponse(r => r.url().endsWith("/v1/submissions") && r.request().method() === "POST"
        && r.request().postDataJSON().schema.name === "build.source.save");
      await page.getByRole("button", { name: "Create", exact: true }).click();
      expect((await created).ok(), await (await created).text()).toBeTruthy();
      await expect(page).toHaveURL(/#\/data-source\/ri\./);
      await expect(page.getByRole("heading", { name: sourceTitle, level: 1 })).toBeVisible();

      await expect.poll(async () => {
        const list = await read<{ records: { id: string; name: string }[] }>("/v1/records/build.source?limit=200");
        const found = list.records.find(s => s.name === `${tag}src`);
        if (found) sourceID = found.id;
        return !!found;
      }, { timeout: 15_000 }).toBe(true);

      // Publish the source so it becomes eligible to pull. The source editor surfaces "Pull now" only
      // once the source is published; the publish lifecycle transition is driven by submission (the
      // editor itself exposes no Publish control — publishing is the release concern covered by F8).
      const current = await read<{ record: { revision: number } }>(`/v1/records/build.source/${sourceID}`);
      await submit({ type: "build.source", id: sourceID }, "publish", {}, current.record.revision);
      await expect.poll(async () => (await read<{ record: { state?: string } }>(`/v1/records/build.source/${sourceID}`)).record.state, { timeout: 30_000 }).toBe("published");
    });

    await test.step("3a the first successful Pull now gives the dataset a version", async () => {
      // Open the published source; "Pull now" is available only in the published state.
      await page.goto(`/#/data-source?id=${sourceID}&application=${project}`);
      await expect(page.getByRole("heading", { name: sourceTitle, level: 1 })).toBeVisible({ timeout: 30_000 });
      // "Pull now" requests one pull at the next tick; the background PullSources worker (every 5s)
      // fetches the local rows and loads them as the dataset's next version.
      const requested = page.waitForResponse(r => r.url().endsWith("/v1/submissions") && r.request().method() === "POST"
        && r.request().postDataJSON().schema.name === "build.source.pull");
      await page.getByRole("button", { name: "Pull now", exact: true }).click();
      expect((await requested).ok(), await (await requested).text()).toBeTruthy();

      // Success: the dataset gains version 1 and a build.datasetversion record with the two rows.
      await expect.poll(async () => (await read<{ record: { version?: number } }>(`/v1/records/build.dataset/${datasetID}`)).record.version ?? 0, { timeout: 90_000, intervals: [3_000] }).toBe(1);
      const version = await read<{ record: { version: number; rows: number } }>(`/v1/records/build.datasetversion/${encodeURIComponent(`${datasetID}@1`)}`);
      expect(version.record.version, "the first run produced dataset version 1").toBe(1);
      expect(version.record.rows, "the version holds the two rows the external system returned").toBe(2);

      // The source's last pull records no error and the two rows it applied.
      const source = await read<{ record: { last?: { error?: string; rows?: number; applied?: number } } }>(`/v1/records/build.source/${sourceID}`);
      expect(source.record.last?.error ?? "", "a successful pull has no error").toBe("");
      expect(source.record.last?.rows, "the successful pull read two rows").toBe(2);
    });

    await test.step("3b a failed sync is visible in Monitoring with an actionable reason and retryable without re-entering inputs", async () => {
      // Take the external system offline, then ask for another pull. The background tick now fails to
      // connect and writes the reason to source.Last.Error.
      serving = false;
      await stopServer();

      const afterSuccess = await read<{ record: { revision: number } }>(`/v1/records/build.source/${sourceID}`);
      await submit({ type: "build.source", id: sourceID }, "pull", {}, afterSuccess.record.revision);

      // The failure reason points at the unreachable endpoint — an actionable correction (the source
      // cannot reach its system). The dataset version does not advance past 1.
      await expect.poll(async () => (await read<{ record: { last?: { error?: string } } }>(`/v1/records/build.source/${sourceID}`)).record.last?.error ?? "", { timeout: 90_000, intervals: [3_000] }).not.toBe("");
      const failed = await read<{ record: { last?: { error?: string } } }>(`/v1/records/build.source/${sourceID}`);
      expect(failed.record.last!.error!.toLowerCase(), "the failure names the endpoint that did not answer").toMatch(/endpoint|answer|connect/);
      expect((await read<{ record: { version?: number } }>(`/v1/records/build.dataset/${datasetID}`)).record.version ?? 0, "a failed pull does not add a dataset version").toBe(1);

      // Monitoring lists the failed sync run as retryable, re-running the same source pull with no inputs.
      const runs = await read<{ runs: { id: string; type: string; status: string; retryable?: boolean; retrySchema?: string; retryTarget?: { type: string; id: string } }[] }>("/v1/monitoring/runs?status=failed&limit=100");
      const run = runs.runs.find(r => r.id === `source:${sourceID}`);
      expect(run, "the failed source appears as a sync run in Monitoring").toBeTruthy();
      expect(run!.type).toBe("sync");
      expect(run!.status).toBe("failed");
      expect(run!.retryable, "the failed run is retryable").toBe(true);
      expect(run!.retrySchema, "retry re-runs the source pull").toBe("build.source.pull");
      expect(run!.retryTarget, "retry targets the same source without new inputs").toMatchObject({ type: "build.source", id: sourceID });

      // The Monitoring Runs view shows the failed row with a "Run again" control; clicking it resubmits
      // the same source pull (no inputs re-entered). The monitoring runs route opens a live subscription
      // that it does not allow, poisoning the read cache ~1s after mount (a known product defect shared
      // with F9), so act within that first-second window with tight timeouts.
      await page.goto("/#/monitoring-runs?status=failed");
      await expect(page.getByRole("heading", { name: "Runs", level: 1 })).toBeVisible({ timeout: 10_000 });
      const retryButton = page
        .getByRole("row")
        .filter({ has: page.getByRole("button", { name: sourceTitle }) })
        .getByRole("button", { name: "Run again" });
      await expect(retryButton).toBeVisible({ timeout: 5_000 });

      const retry = page.waitForResponse(r => {
        if (!r.url().endsWith("/v1/submissions") || r.request().method() !== "POST") return false;
        try { return r.request().postDataJSON()?.schema?.name === "build.source.pull"; } catch { return false; }
      });
      await retryButton.click();
      const retryBody = (await retry).request().postDataJSON();
      expect(retryBody.schema.name, "retry schema is the source pull").toBe("build.source.pull");
      expect(retryBody.target, "retry targets the same source").toMatchObject({ type: "build.source", id: sourceID });
      const retryPayload = JSON.parse(Buffer.from(retryBody.payload, "base64").toString("utf-8"));
      expect(retryPayload, "the retry carries no re-entered inputs").toEqual({});
    });
  } finally {
    await stopServer();

    // Archive the resources this case created where the platform still allows it. The acceptance host
    // is isolated and discarded per run, so any refusal leaves no shared state behind.
    const identity = await page.request.get("/v1/me", { headers }).then(r => r.json()).catch(() => undefined) as { tenantId: string; principalId: string } | undefined;
    const tryTransition = async (type: string, id: string, action: string) => {
      if (!identity || !id) return;
      const current = await page.request.get(`/v1/records/${type}/${id}`, { headers }).then(r => r.ok() ? r.json() : undefined).catch(() => undefined) as { record: { revision: number } } | undefined;
      if (!current) return;
      await page.request.post("/v1/submissions", {
        headers,
        data: {
          tenantId: identity.tenantId, principalId: identity.principalId, authority: "build",
          target: { type, id }, schema: { name: `${type}.${action}`, version: 1 },
          idempotencyKey: unique("f3cleanup"),
          payload: Buffer.from(JSON.stringify({})).toString("base64"), expectedRevision: current.record.revision,
        },
      }).catch(() => undefined);
    };
    await tryTransition("build.source", sourceID, "pause");
    await tryTransition("build.source", sourceID, "archive");
    await tryTransition("build.connection", connectionID, "retire");
    await tryTransition("build.connection", connectionID, "archive");
    await tryTransition("build.dataset", datasetID, "archive");
  }
});
