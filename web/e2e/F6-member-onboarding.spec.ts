import { expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 15_000 });

test("F6 an admin invites a member, grants a role, and the member sees only what they were authorised for on first login", async ({ page, browser }) => {
  test.setTimeout(300_000);

  const id = unique("m");
  const subject = `user:${id}@example.test`;
  const adminHeaders = { authorization: "Bearer admin" };

  const read = async <T,>(pg: typeof page, path: string, headers = adminHeaders): Promise<T> => {
    const response = await pg.request.get(path, { headers });
    expect(response.ok(), await response.text()).toBeTruthy();
    return response.json();
  };

  await test.step("step 1: admin navigates to Control Panel → Members and invites the member via the Invite member sheet", async () => {
    await signIn(page, "admin", "/members");

    const header = page.getByRole("heading", { name: "Members and access", level: 1 });
    await expect(header).toBeVisible({ timeout: 20_000 });

    await page.getByRole("button", { name: "Invite member", exact: true }).click();

    const sheet = page.getByRole("dialog", { name: "Invite member", exact: true });
    await expect(sheet).toBeVisible();

    await sheet.getByRole("textbox", { name: /Member ID/ }).fill(id);
    await sheet.getByRole("textbox", { name: /Signs in as/ }).fill(subject);

    const invited = page.waitForResponse(
      (r) =>
        r.url().endsWith("/v1/submissions") &&
        r.request().method() === "POST" &&
        r.request().postDataJSON().schema.name === "platform.member.invite",
    );
    await sheet.getByRole("button", { name: "Invite", exact: true }).click();
    const inviteResponse = await invited;
    const inviteAnswer = await inviteResponse.json();
    expect(inviteResponse.ok(), JSON.stringify(inviteAnswer)).toBeTruthy();
    expect(inviteAnswer.error).toBeUndefined();

    await expect(sheet).toBeHidden();

    // Assert: the new member row appears in the table with "invited" status.
    const row = page.getByRole("row", { name: new RegExp(id) });
    await expect(row).toBeVisible({ timeout: 20_000 });
    await expect(row.getByText("invited", { exact: true })).toBeVisible();
  });

  await test.step("step 2: admin opens the member detail and grants build:builder so the member can open the Build portal", async () => {
    // Click the member row to open the member detail panel.
    await page.getByRole("row", { name: new RegExp(id) }).click();

    // The member detail opens in a side panel; its Roles section is titled by an <h2>.
    await expect(page.getByRole("heading", { name: "Roles", exact: true })).toBeVisible({ timeout: 15_000 });

    await page.getByRole("button", { name: "Add role", exact: true }).click();

    const grantSheet = page.getByRole("dialog", { name: "Add role", exact: true });
    await expect(grantSheet).toBeVisible();

    // Pick app "build" and role "builder".
    await grantSheet.getByRole("combobox", { name: "App" }).selectOption("build");
    await grantSheet.getByRole("combobox", { name: "Role" }).selectOption("builder");

    const granted = page.waitForResponse(
      (r) =>
        r.url().endsWith("/v1/submissions") &&
        r.request().method() === "POST" &&
        r.request().postDataJSON().schema.name === "platform.member.grant",
    );
    await grantSheet.getByRole("button", { name: "Grant", exact: true }).click();
    const grantResponse = await granted;
    const grantAnswer = await grantResponse.json();
    expect(grantResponse.ok(), JSON.stringify(grantAnswer)).toBeTruthy();
    expect(grantAnswer.error).toBeUndefined();

    await expect(grantSheet).toBeHidden();

    // Assert: the grant appears in the member record via the API (admin can read /v1/members).
    await expect.poll(
      async () => {
        const members = await read<{ id: string; grants?: { app: string; role: string }[] }[]>(page, "/v1/members");
        const member = members.find((m) => m.id === id);
        return member?.grants?.some((g) => g.app === "build" && g.role === "builder") ?? false;
      },
      { timeout: 20_000 },
    ).toBe(true);
  });

  const memberPage = await (await browser.newContext()).newPage();
  try {
    await test.step("step 3 (success criteria): the invited member signs in and sees only their authorised scope", async () => {
      await signIn(memberPage, subject);

      // Positive assertion: the member's /v1/me reports the "build" app among their apps,
      // confirming the grant took effect and the member is now active.
      const me = await read<{ apps: { id: string; role: string }[] }>(memberPage, "/v1/me", { authorization: `Bearer ${subject}` });
      expect(
        me.apps.some((a) => a.id === "build" && a.role === "builder"),
        "invited member should hold build:builder in their apps after the grant",
      ).toBe(true);

      // Positive assertion (UI): the member's home shows the "Build tools" section —
      // the apps their build:builder role authorises.
      await expect(memberPage.getByRole("heading", { name: "Build tools", exact: true })).toBeVisible({ timeout: 20_000 });
      await expect(memberPage.getByRole("button", { name: /^Ontology Manager/ })).toBeVisible();

      // Negative assertion (UI): the member has no platform:admin, so the Control Panel
      // is absent from the application rail (it only shows to admins/auditors).
      await expect(memberPage.getByRole("button", { name: "Control Panel", exact: true })).toHaveCount(0);

      // Negative assertion (API): /v1/members returns 403 for a non-admin member.
      const membersResponse = await memberPage.request.get("/v1/members", { headers: { authorization: `Bearer ${subject}` } });
      expect(membersResponse.status()).toBe(403);
    });
  } finally {
    await memberPage.context().close();
  }

  await test.step("cleanup: offboard the member this case created", async () => {
    // Offboard via API to keep the tenant clean; successor is empty (no delegations).
    const identity = await read<{ tenantId: string; principalId: string }>(page, "/v1/me");
    const response = await page.request.post("/v1/submissions", {
      headers: adminHeaders,
      data: {
        tenantId: identity.tenantId,
        principalId: identity.principalId,
        authority: "platform",
        target: { type: "platform.member", id },
        schema: { name: "platform.member.offboard", version: 1 },
        idempotencyKey: unique("f6off"),
        payload: Buffer.from(JSON.stringify({ reason: "F6 acceptance test cleanup", successor: "" })).toString("base64"),
      },
    });
    const answer = await response.json();
    expect(response.ok(), JSON.stringify(answer)).toBeTruthy();
  });
});
