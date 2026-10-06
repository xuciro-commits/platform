// Who is signed in and which tenant they opened, remembered for this browser
// session only (ADR-0018). Development identities come from the host's
// `/v1/sign-in`; a production session comes from the identity provider.
import type { Api } from "@platform/kernel";

/** A development identity of a host on development tokens (GET /v1/sign-in); generated from the host (ADR-0023). */
export type Identity = Api.Identity;

export const IDENTITY_KEY = "workspace:identity";
export const TENANT_KEY = "workspace:tenant";

export const remembered = (key: string) => { try { return sessionStorage.getItem(key) ?? undefined; } catch { return undefined; } };
export const remember = (key: string, value: string) => { try { sessionStorage.setItem(key, value); } catch { /* storage unavailable */ } };
export const forget = (key: string) => { try { sessionStorage.removeItem(key); } catch { /* storage unavailable */ } };

/** The development identity that sees most: an administrator if there is one. */
export const preferred = (ids: Identity[]) => [...ids].sort((a, b) => Object.keys(b.roles).length - Object.keys(a.roles).length)[0]?.token ?? "";
