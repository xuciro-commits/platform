import type { ResourceKind } from "../projects/resources";

export type JointChoice = { kind: ReleaseKind; id: string };
export const ownerDraft = (choice: JointChoice): JointChoice => ({ ...choice, kind: choice.kind === "table" ? "compute" : choice.kind });
export const jointKey = (choice: JointChoice) => `${ownerDraft(choice).kind}/${choice.id}`;
export const merged = (current: JointChoice[], incoming: JointChoice[]): JointChoice[] => {
  const seen = new Set<string>();
  const out: JointChoice[] = [];
  for (const choice of [...current, ...incoming]) {
    if (!choice.id || seen.has(jointKey(choice))) continue;
    seen.add(jointKey(choice));
    out.push(choice);
  }
  return out;
};

export async function applicationDraftSelection(roots: JointChoice[], referenced: (root: JointChoice) => Promise<JointChoice[]>): Promise<JointChoice[]> {
  let selection = merged([], roots);
  for (const root of roots.filter(draft => draft.kind === "app")) {
    selection = merged(selection, await referenced(root));
  }
  return selection;
}

export type ReleaseKind = Exclude<ResourceKind, "action" | "automation"> | "app";
export function releaseSelectionProblem(drafts: readonly JointChoice[]): string | undefined {
  if (drafts.length > 128) return "A joint candidate accepts at most 128 drafts. Select a supported application closure.";
  if (new TextEncoder().encode(JSON.stringify({ drafts: drafts.map(ownerDraft) })).length > releaseCommandBytes) return "The release selection exceeds the original 65536-byte request budget. No drafts were dropped.";
}
export function releaseDraftsSelection(value: string | undefined, kinds: readonly string[]): { drafts: JointChoice[]; error?: string } {
  if (!value) return { drafts: [] };
  const invalid = () => ({ drafts: [], error: "The release selection is invalid or exceeds its budget. Reopen review from the application." });
  if (new TextEncoder().encode(value).length > releaseCommandBytes) return invalid();
  let selected: unknown;
  if (value.trimStart().startsWith("[")) {
    try { selected = JSON.parse(value); } catch { return invalid(); }
  } else {
    selected = value.split(",").map((item) => {
      const separator = item.indexOf(":");
      return { kind: item.slice(0, separator), id: separator < 0 ? "" : item.slice(separator + 1) };
    });
  }
  if (!Array.isArray(selected) || selected.length > 128) return invalid();
  const out: JointChoice[] = [];
  for (const item of selected) {
    if (!item || typeof item !== "object" || typeof item.kind !== "string" || typeof item.id !== "string"
      || !kinds.includes(item.kind) || !item.id) return invalid();
    const choice = { kind: item.kind as ReleaseKind, id: item.id };
    if (!out.some(existing => jointKey(existing) === jointKey(choice))) out.push(choice);
  }
  const problem = releaseSelectionProblem(out);
  return problem ? { drafts: [], error: problem } : { drafts: out };
}
export const releaseCommandBytes = 128 * 512;
export const releaseDraftsValue = (drafts: readonly JointChoice[]): string => JSON.stringify(drafts.map(({ kind, id }) => ({ kind, id })));

