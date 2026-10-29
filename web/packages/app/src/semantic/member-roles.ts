import type { Api } from "@platform/kernel";

export function memberRoles(member: Pick<Api.Member, "roles" | "grants">, app: string): string[] {
  return [...new Set([member.roles[app], ...(member.grants ?? []).filter(grant => grant.app === app).map(grant => grant.role)].filter((role): role is string => !!role))];
}

