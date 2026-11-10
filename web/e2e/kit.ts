import { expect, test, type Page } from "@playwright/test";

export type { Page };

export { expect, test };

let counter = 0;

export const unique = (prefix: string) => `${prefix}${Date.now().toString(36)}${counter++}`;

export function listDetailPage(object: string, fields: string[], actions: string[] = []) {
  const ref = { app: object.split(".")[0]!, kind: "object", name: object };
  const refs = fields.map(name => ({ name }));
  const binding = (id: string, widget: string, ports: Record<string, string>) =>
    ({ id, owner: `binding:${id}`, kind: "record", object: ref, model: { type: object, fields: refs }, fields: refs, ports: { ...ports, widget } });
  return {
    format: 4, object,
    dataBindings: [binding("list", "table", { parentable: "true", selectionMode: "write" }), binding("detail", "detail", { selectionMode: "read" })],
    actionTriggers: [{ id: "page", binding: "page", actions: actions.map(name => ({ app: name.split(".")[0]!, kind: "action", name })) },
      { id: "list", owner: "binding:list", binding: "list" }, { id: "detail", owner: "binding:detail", binding: "detail" }],
    inputSchema: {},
    scopeVisibility: { root: "page", owners: { list: "binding:list", detail: "binding:detail" },
      scopes: [{ id: "page", kind: "page" }, { id: "binding:list", parent: "page", kind: "page", binding: "list" }, { id: "binding:detail", parent: "page", kind: "page", binding: "detail" }] },
    widgetSpec: { format: "platform.page.web.v1", body: { document: null, layout: "list-detail",
      sections: [{ binding: "list", id: "list", triggers: ["list"] }, { binding: "detail", id: "detail", triggers: ["detail"] }] } },
  };
}

export async function signIn(page: Page, subject: string, route = "") {
  await page.addInitScript(token => sessionStorage.setItem("workspace:identity", token), subject);
  await page.goto(`/#${route}`);
  await expect(page.getByRole("button", { name: "Home", exact: true }).first()).toBeVisible();
}

export function acceptedClient(page: Page, subject = "admin", tenant?: string) {
  const headers = { authorization: `Bearer ${subject}`, ...(tenant ? { "Platform-Tenant": tenant } : {}) };
  const read = async <T,>(path: string): Promise<T> => {
    const response = await page.request.get(path, { headers });
    expect(response.ok(), await response.text()).toBeTruthy();
    return response.json();
  };
  const submit = async (type: string, id: string, action: string, payload: unknown, expectedRevision?: number | null) => {
    const identity = await read<{ tenantId: string; principalId: string }>("/v1/me");
    const declarations = await read<{ dataClass: string; authorityId: string }[]>("/v1/declarations");
    const authority = declarations.find(declaration => declaration.dataClass === type)?.authorityId;
    expect(authority, `an accepted writer owns ${type}`).toBeTruthy();
    const schema = `${type}.${action}`;
    const catalog = await read<{ schema: string; schemaVersion?: { version: string; family: number } }[]>("/v1/actions");
    const version = catalog.find(entry => entry.schema === schema)?.schemaVersion;
    const revision = expectedRevision === null ? undefined : expectedRevision ?? (action === "create" ? 0 : (await read<{ record: { revision: number } }>(`/v1/records/${type}/${encodeURIComponent(id)}`)).record.revision);
    const response = await page.request.post("/v1/submissions", {
      headers: { ...headers, ...(version ? { "Platform-Schema-Version": version.version } : {}) },
      data: {
        tenantId: identity.tenantId, principalId: identity.principalId, authority, target: { type, id }, schema: { name: schema, version: version?.family ?? 1 },
        idempotencyKey: unique("accepted"), expectedRevision: revision,
        payload: Buffer.from(JSON.stringify(payload)).toString("base64"),
      },
    });
    const answer = await response.json();
    expect(response.ok(), JSON.stringify(answer)).toBeTruthy();
    expect(answer.error).toBeUndefined();
    expect(answer.record?.changeId).toEqual(expect.any(String));
    return answer.record as { changeId: string };
  };
  const publish = async (drafts: { kind: string; id: string }[]) => {
    const response = await page.request.post("/v1/releases/preview", { headers, data: { drafts } });
    expect(response.ok(), await response.text()).toBeTruthy();
    const preview = await response.json();
    expect(preview.diagnostic, JSON.stringify(preview)).toBeUndefined();
    expect(preview.candidateId).toEqual(expect.any(String));
    for (const path of ["/v1/releases/candidates", "/v1/releases/active"]) {
      const answer = await page.request.post(path, {
        headers, data: { ...(path.endsWith("candidates") ? { drafts } : {}), candidateId: preview.candidateId, key: unique("release") },
      });
      expect(answer.ok(), await answer.text()).toBeTruthy();
    }
    return preview.candidateId as string;
  };
  return { headers, read, submit, publish };
}
