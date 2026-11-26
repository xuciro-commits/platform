import { useMemo } from "react";
import { type Api, useRead } from "../../host";
import { t } from "../../i18n";

export type DurationPreset = "permanent" | "7d" | "30d" | "90d" | "custom";
export type IsoDate = `${number}${number}${number}${number}-${number}${number}-${number}${number}`;

export function asIsoDate(value?: string): IsoDate | undefined {
  return value && /^\d{4}-\d{2}-\d{2}$/.test(value) ? (value as IsoDate) : undefined;
}

export type UnitChoice = {
  value: string;
  unit: string;
  structure: string;
  name: string;
  structureName: string;
  label: string;
};

type Span = { from?: string; until?: string; periods?: Api.Period[] };

export function todayISO(timeZone?: string): IsoDate {
  if (!timeZone) return new Date().toISOString().slice(0, 10) as IsoDate;
  try {
    return new Intl.DateTimeFormat("en-CA", {
      timeZone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(new Date()) as IsoDate;
  } catch {
    return new Date().toISOString().slice(0, 10) as IsoDate;
  }
}

export function addDaysISO(days: number, base?: string): IsoDate {
  const start = base ? new Date(`${base}T00:00:00Z`) : new Date();
  const next = new Date(Date.UTC(start.getUTCFullYear(), start.getUTCMonth(), start.getUTCDate() + days));
  return next.toISOString().slice(0, 10) as IsoDate;
}

export function untilFromPreset(preset: DurationPreset, customUntil: string, baseDay?: string): string {
  if (preset === "permanent") return "";
  if (preset === "7d") return addDaysISO(7, baseDay);
  if (preset === "30d") return addDaysISO(30, baseDay);
  if (preset === "90d") return addDaysISO(90, baseDay);
  return customUntil;
}

export function daysUntil(until?: string, today = todayISO()): number | null {
  if (!until) return null;
  const a = new Date(`${today}T00:00:00Z`).getTime();
  const b = new Date(`${until}T00:00:00Z`).getTime();
  if (Number.isNaN(a) || Number.isNaN(b)) return null;
  return Math.round((b - a) / 86_400_000);
}

export function spanAlive(span: Span, day: string): boolean {
  const list = span.periods?.length ? span.periods : [span];
  return list.some((period) => (!period.from || period.from <= day) && (!period.until || day < period.until));
}

export function useUnitChoices() {
  const org = useRead<Api.OrgSeed>("/v1/organization");
  return useMemo(() => {
    const map = new Map<string, string>();
    const choices: UnitChoice[] = [];
    if (org.status !== "ready") return { choices, unitName: (id: string) => map.get(id) ?? id };
    const day = todayISO();
    const structures = new Map(org.data.structures.map((item) => [item.id, item.name || item.id]));
    const defaultStructure = org.data.structures[0]?.id ?? "";
    for (const unit of org.data.units) {
      map.set(unit.id, unit.name || unit.id);
    }
    const seen = new Set<string>();
    for (const edge of org.data.edges) {
      if (!spanAlive(edge, day)) continue;
      const unit = org.data.units.find((candidate) => candidate.id === edge.unit && spanAlive(candidate, day));
      if (!unit) continue;
      const key = `${edge.structure}|${unit.id}`;
      if (seen.has(key)) continue;
      seen.add(key);
      const structureName = structures.get(edge.structure) ?? edge.structure;
      const name = unit.name || unit.id;
      choices.push({
        value: key,
        unit: unit.id,
        structure: edge.structure,
        name,
        structureName,
        label: org.data.structures.length > 1 ? `${name} · ${structureName}` : name,
      });
    }
    if (defaultStructure) {
      for (const unit of org.data.units) {
        if (!spanAlive(unit, day)) continue;
        const key = `${defaultStructure}|${unit.id}`;
        if (seen.has(key)) continue;
        seen.add(key);
        const structureName = structures.get(defaultStructure) ?? defaultStructure;
        const name = unit.name || unit.id;
        choices.push({
          value: key,
          unit: unit.id,
          structure: defaultStructure,
          name,
          structureName,
          label: org.data.structures.length > 1 ? `${name} · ${structureName}` : name,
        });
      }
    }
    choices.sort((a, b) => a.label.localeCompare(b.label));
    return { choices, unitName: (id: string) => map.get(id) ?? id };
  }, [org]);
}

export function useMemberDirectoryMap() {
  const directory = useRead<Api.MemberOption[]>("/v1/member-directory");
  return useMemo(() => {
    const names = new Map<string, string>();
    const options: { value: string; label: string }[] = [];
    if (directory.status === "ready") {
      for (const item of directory.data) {
        const label = item.name || item.id;
        names.set(item.id, label);
        options.push({ value: item.id, label: item.name && item.name !== item.id ? `${item.name} (${item.id})` : label });
      }
    }
    return {
      options,
      nameOf: (id?: string) => (id ? names.get(id) ?? id : ""),
    };
  }, [directory]);
}

export function grantSourceSummary(grant: Api.Grant, memberName: (id?: string) => string): {
  kind: "team" | "project" | "delegated" | "direct";
  label: string;
  revocable: boolean;
} {
  const by = grant.by ?? "";
  if (by.startsWith("team:")) {
    const teamName = grant.reason || by.slice(5);
    return { kind: "team", label: `${t("Team")}: ${teamName}`, revocable: false };
  }
  if (by.startsWith("project:")) {
    const projectName = grant.reason || by.slice(8);
    return { kind: "project", label: `${t("Project")}: ${projectName}`, revocable: false };
  }
  if ((grant.reason ?? "").startsWith("delegated")) {
    return {
      kind: "delegated",
      label: by ? t("Delegated by {member}").replace("{member}", memberName(by)) : t("Delegated"),
      revocable: true,
    };
  }
  return {
    kind: "direct",
    label: by ? t("Granted by {member}").replace("{member}", memberName(by)) : t("Direct grant"),
    revocable: true,
  };
}

export function scopeName(scope?: string): string {
  switch (scope) {
    case "tenant":
      return t("All records");
    case "below":
      return t("Unit and sub-units");
    case "unit":
      return t("Assigned unit");
    case "team":
      return t("Same-unit team");
    case "own":
      return t("Own records");
    case "none":
      return t("No records");
    default:
      return scope ? t(scope) : t("All records");
  }
}

export function layerName(name: string): string {
  switch (name) {
    case "tenant":
      return t("Tenant isolation");
    case "principal":
      return t("Account standing");
    case "deny":
      return t("Explicit deny policies");
    case "verb":
      return t("Role & action grant");
    case "row":
      return t("Record relationship scope");
    case "fields":
      return t("Field permissions");
    case "derived":
      return t("Source data closure");
    case "markings":
      return t("Security markings");
    case "allow":
      return t("Final decision");
    default:
      return t(name);
  }
}

export function scopeRank(scope?: string): number {
  switch (scope) {
    case "tenant":
      return 5;
    case "below":
      return 4;
    case "unit":
      return 3;
    case "team":
      return 2;
    case "own":
      return 1;
    case "none":
      return 0;
    default:
      return 5;
  }
}

export function roleTitle(role: string): string {
  switch (role) {
    case "*":
      return t("Everyone");
    case "owner":
      return t("Owner");
    case "editor":
      return t("Editor");
    case "viewer":
      return t("Viewer");
    case "discoverer":
      return t("Discoverer");
    case "admin":
      return t("Administrator");
    case "auditor":
      return t("Auditor");
    case "builder":
      return t("Builder");
    case "publisher":
      return t("Publisher");
    case "integrator":
      return t("Integrator");
    case "user":
      return t("Member");
    default:
      return t(role);
  }
}
