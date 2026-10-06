import { expect, test } from "@playwright/test";

test("a rejected host member can sign out after refresh and choose another account", async ({ page, baseURL }) => {
  const origin = baseURL!;
  const issuer = `${origin}/test-identity/`;
  const idToken = "test-id-token";
  let discoveryUnavailable = true;
  await page.route("**/v1/sign-in", route => route.fulfill({ json: { issuer, client: "workspace-test" } }));
  await page.route("**/v1/me", route => route.fulfill({ status: 401, json: { error: "not a member" } }));
  await page.route(`${issuer}.well-known/openid-configuration`, route => discoveryUnavailable
    ? route.fulfill({ status: 503 })
    : route.fulfill({ json: {
      authorization_endpoint: `${issuer}authorize`, token_endpoint: `${issuer}token`, end_session_endpoint: `${issuer}logout`,
    } }));
  let logout: URL | undefined;
  await page.route(`${issuer}logout?**`, async route => {
    logout = new URL(route.request().url());
    await route.fulfill({ status: 302, headers: { location: origin } });
  });
  await page.route(`${issuer}authorize?**`, route => route.fulfill({ contentType: "text/html", body: "<h1>Choose an account</h1>" }));
  // Seed only this tab's rejected session, as an identity provider callback would.
  await page.addInitScript(({ idToken }) => {
    if (sessionStorage.getItem("test:session-seeded")) return;
    sessionStorage.setItem("test:session-seeded", "true");
    sessionStorage.setItem("oidc:session", JSON.stringify({ accessToken: "rejected-access-token", idToken,
      email: "manager@hotel.test", expiresAt: Date.now() + 3_600_000 }));
    sessionStorage.setItem("workspace:identity", "previous-development-identity");
    sessionStorage.setItem("workspace:tenant", "previous-tenant");
  }, { idToken });
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "manager@hotel.test is not a member of this host." })).toBeVisible();
  await page.reload();
  await page.getByRole("button", { name: "Sign out and switch account", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText("Could not reach the sign-in provider. Retry signing out.");
  discoveryUnavailable = false;
  await page.getByRole("button", { name: "Sign out and switch account", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Choose an account" })).toBeVisible();
  expect(logout?.searchParams.get("id_token_hint")).toBe(idToken);
  expect(logout?.searchParams.get("post_logout_redirect_uri")).toBe(`${origin}/`);
  expect(await page.evaluate(() => ["oidc:session", "workspace:identity", "workspace:tenant"].map(key => sessionStorage.getItem(key)))).toEqual([null, null, null]);
});
