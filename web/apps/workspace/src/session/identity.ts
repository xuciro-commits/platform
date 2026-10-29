import type { Api } from "@platform/kernel";

export type Identity = Api.Identity;

export const IDENTITY_KEY = "workspace:identity";
export const TENANT_KEY = "workspace:tenant";

export const remembered = (key: string) => { try { return sessionStorage.getItem(key) ?? undefined; } catch { return undefined; } };
export const remember = (key: string, value: string) => { try { sessionStorage.setItem(key, value); } catch { /* storage unavailable */ } };
export const forget = (key: string) => { try { sessionStorage.removeItem(key); } catch { /* storage unavailable */ } };

export const preferred = (ids: Identity[]) => [...ids].sort((a, b) => Object.keys(b.roles).length - Object.keys(a.roles).length)[0]?.token ?? "";
