import { expect, test } from "vitest";
import { codeChallenge } from "./oidc";

test("S256 code challenge matches RFC 7636 appendix B", async () => {
  expect(await codeChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")).toBe("E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM");
});

test("cached sessions and PKCE callbacks belong to their issuer and client", async () => {
  const { currentSession } = await import("./oidc");
  const { vi } = await import("vitest");
  const memory = new Map<string, string>();
  const storage = { getItem: (key: string) => memory.get(key) ?? null, setItem: (key: string, value: string) => memory.set(key, value), removeItem: (key: string) => memory.delete(key) };
  const config = { issuer: "http://localhost:8495/auth/v1/", clientId: "platform-web", redirectUri: "http://localhost:8495/" };
  vi.stubGlobal("sessionStorage", storage);
  vi.stubGlobal("location", { search: "", pathname: "/", hash: "" });
  vi.stubGlobal("history", { replaceState: vi.fn() });
  const fetcher = vi.fn(); vi.stubGlobal("fetch", fetcher);
  try {
    const session = { ...config, accessToken: "current", expiresAt: Date.now() + 60_000, email: "user@platform.test" };
    memory.set("oidc:session", JSON.stringify(session));
    expect(await currentSession(config)).toMatchObject({ accessToken: "current" });
    for (const old of [{ ...session, issuer: "http://localhost:8480/auth/v1/" }, { ...session, clientId: "old-client" }, { accessToken: "legacy", expiresAt: Date.now() + 60_000 }]) {
      memory.set("oidc:session", JSON.stringify(old));
      expect(await currentSession(config)).toBeNull();
      expect(memory.has("oidc:session")).toBe(false);
    }
    vi.stubGlobal("location", { search: "?code=old&state=same", pathname: "/", hash: "" });
    memory.set("oidc:pending", JSON.stringify({ issuer: "http://old.test/", clientId: config.clientId, state: "same", verifier: "old" }));
    expect(await currentSession(config)).toBeNull();
    expect(fetcher).not.toHaveBeenCalled();
  } finally { vi.unstubAllGlobals(); }
});
