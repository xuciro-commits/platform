import type { Api } from "../host";
import { compareVersions } from "./PackageCard";

export type ResolvedDependency = {
  requirement: string;
  id: string;
  title: string;
  version: string;
  namespace: string;
  url?: string;
  sha256?: string;
  digest?: string;
  action: "install" | "upgrade";
  source: "local" | "remote";
  installedVersion?: string;
};

export type DependencyResolution = {
  steps: ResolvedDependency[];
  unresolved: string[];
};

type ParsedVersion = {
  major: number;
  minor: number;
  patch: number;
  prerelease: string;
};

function parseVersion(value: string): ParsedVersion | null {
  const [withoutBuild] = value.trim().split("+");
  const dash = withoutBuild.indexOf("-");
  const numberPart = dash >= 0 ? withoutBuild.slice(0, dash) : withoutBuild;
  const prerelease = dash >= 0 ? withoutBuild.slice(dash + 1) : "";
  const parts = numberPart.split(".");
  if (parts.length !== 3) return null;
  const nums = parts.map((part) => (/^\d+$/.test(part) ? Number(part) : Number.NaN));
  if (nums.some((n) => Number.isNaN(n))) return null;
  return { major: nums[0], minor: nums[1], patch: nums[2], prerelease };
}

export function parseDependency(requirement: string): { id: string; constraint: string } {
  const at = requirement.indexOf("@");
  if (at < 0) return { id: requirement.trim(), constraint: "" };
  return { id: requirement.slice(0, at).trim(), constraint: requirement.slice(at + 1).trim() };
}

export function matchesVersionConstraint(version: string, constraint: string): boolean {
  const parsedVersion = parseVersion(version);
  if (!parsedVersion) return false;
  if (!constraint || constraint === "*") return parsedVersion.prerelease === "";
  for (const alternative of constraint.split("||")) {
    if (parsedVersion.prerelease !== "" && !alternative.includes("-")) continue;
    let matched = true;
    for (const term of alternative.trim().split(/\s+/).filter(Boolean)) {
      let op = "=";
      let base = term;
      for (const prefix of [">=", "<=", ">", "<", "^", "~", "="]) {
        if (term.startsWith(prefix)) {
          op = prefix;
          base = term.slice(prefix.length);
          break;
        }
      }
      const parsed = parseVersion(base);
      if (!parsed) {
        matched = false;
        break;
      }
      const order = compareVersions(version, base);
      if (op === "=") matched = matched && order === 0;
      else if (op === ">") matched = matched && order > 0;
      else if (op === ">=") matched = matched && order >= 0;
      else if (op === "<") matched = matched && order < 0;
      else if (op === "<=") matched = matched && order <= 0;
      else if (op === "~" || op === "^") {
        let upper = `${parsed.major}.${parsed.minor + 1}.0`;
        if (op === "^") {
          if (parsed.major > 0) upper = `${parsed.major + 1}.0.0`;
          else if (parsed.minor === 0) upper = `0.0.${parsed.patch + 1}`;
        }
        matched = matched && order >= 0 && compareVersions(version, upper) < 0;
      }
    }
    if (matched) return true;
  }
  return false;
}

export async function resolvePackageDependencies(
  descriptor: Api.PackageDescriptor,
  localViews: Api.PackageView[],
  libraries: Api.PackageLibraryView[],
  inspect: (body: Api.PackageInspectRequest) => Promise<Api.PackagePreview>,
): Promise<DependencyResolution> {
  const installedById = new Map<string, Api.InstalledPackage>();
  for (const view of localViews) {
    if (view.installed && view.installed.state !== "retired") {
      installedById.set(view.descriptor.id, view.installed);
    }
  }
  const remoteEntries: Api.PackageCatalogEntry[] = [];
  for (const lib of libraries) {
    for (const entry of lib.entries ?? []) remoteEntries.push(entry);
  }

  const steps: ResolvedDependency[] = [];
  const unresolved: string[] = [];
  const resolvedIds = new Set<string>([descriptor.id]);
  const visiting = new Set<string>([descriptor.id]);

  const visit = async (requires: string[] | undefined) => {
    for (const requirement of requires ?? []) {
      const { id, constraint } = parseDependency(requirement);
      if (!id || resolvedIds.has(id)) continue;
      if (visiting.has(id)) {
        unresolved.push(requirement);
        continue;
      }
      const installed = installedById.get(id);
      if (installed && installed.state === "active" && matchesVersionConstraint(installed.version, constraint)) {
        resolvedIds.add(id);
        continue;
      }
      visiting.add(id);
      const localMatch = localViews
        .filter((view) => view.descriptor.id === id && matchesVersionConstraint(view.descriptor.version, constraint))
        .sort((a, b) => compareVersions(b.descriptor.version, a.descriptor.version))[0];
      if (localMatch) {
        await visit(localMatch.descriptor.requires);
        steps.push({
          requirement,
          id,
          title: localMatch.descriptor.title || id,
          version: localMatch.descriptor.version,
          namespace: localMatch.descriptor.namespace || id,
          action: installed ? "upgrade" : "install",
          source: "local",
          installedVersion: installed?.version,
        });
        resolvedIds.add(id);
        visiting.delete(id);
        continue;
      }
      const remoteMatch = remoteEntries
        .filter((entry) => entry.id === id && matchesVersionConstraint(entry.version, constraint))
        .sort((a, b) => compareVersions(b.version, a.version))[0];
      if (!remoteMatch) {
        unresolved.push(requirement);
        visiting.delete(id);
        continue;
      }
      try {
        const preview = await inspect({ url: remoteMatch.url, expectedSha256: remoteMatch.sha256 });
        await visit(preview.descriptor.requires);
        steps.push({
          requirement,
          id: preview.descriptor.id,
          title: preview.descriptor.title || remoteMatch.title || id,
          version: preview.descriptor.version,
          namespace: preview.descriptor.namespace || id,
          url: remoteMatch.url,
          sha256: preview.artifactSha256 || remoteMatch.sha256,
          digest: preview.digest,
          action: installed ? "upgrade" : "install",
          source: "remote",
          installedVersion: installed?.version,
        });
        resolvedIds.add(id);
      } catch {
        unresolved.push(requirement);
      }
      visiting.delete(id);
    }
  };

  await visit(descriptor.requires);
  return { steps, unresolved };
}
