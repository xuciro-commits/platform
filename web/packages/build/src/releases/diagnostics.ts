// The host refuses a candidate in one English sentence. The review reads the
// sentences it knows, says them in the member's language and names the place
// in the editors where the fix is made; unknown sentences are shown as they
// came. The host's checks stay the authority; nothing here guesses a repair.
import { t } from "@platform/ui";

export type Diagnosis = { text: string; fix?: { label: string; view: string; params: Record<string, string> } };

type Rule = { match: RegExp; text: (m: RegExpMatchArray) => string; fix?: (m: RegExpMatchArray, ids: { application?: string }) => Diagnosis["fix"] };

const navigation = (ids: { application?: string }) => ids.application ? { label: t("Open the module's navigation"), view: "module", params: { id: ids.application, application: ids.application, focus: "navigation" } } : undefined;

const rules: Rule[] = [
  { match: /the group "(.+?)" holds no page/, text: (m) => t("The group “{group}” holds no page. Put a page under it or remove the group.", { group: m[1]! }), fix: (_, ids) => navigation(ids) },
  { match: /a group needs a title/, text: () => t("A navigation group has no title."), fix: (_, ids) => navigation(ids) },
  { match: /the group "(.+?)" names the page "(.+?)", which the application does not hold/, text: (m) => t("The group “{group}” lists the page “{page}”, which this module does not hold.", { group: m[1]!, page: m[2]! }), fix: (_, ids) => navigation(ids) },
  { match: /the page "(.+?)" is under "(.+?)" and "(.+?)"/, text: (m) => t("The page “{page}” is under two groups: “{a}” and “{b}”.", { page: m[1]!, a: m[2]!, b: m[3]! }), fix: (_, ids) => navigation(ids) },
  { match: /application (\S+): it holds no page/, text: (m) => t("The module {name} holds no page yet.", { name: m[1]! }), fix: (_, ids) => navigation(ids) },
  { match: /application (\S+): no published resource (\S+)/, text: (m) => t("The module {name} depends on {resource}, which is not published. Add that draft to the joint candidate or publish it first.", { name: m[1]!, resource: m[2]! }) },
  { match: /application header (.+)/, text: (m) => t("The module's header is not valid: {why}.", { why: m[1]! }), fix: (_, ids) => ids.application ? { label: t("Open the header"), view: "module", params: { id: ids.application, application: ids.application, focus: "header" } } : undefined },
];

/** Reads a host diagnostic; `ids.application` is the module the review was opened from. */
export function diagnose(diagnostic: string, ids: { application?: string }): Diagnosis {
  for (const rule of rules) {
    const m = diagnostic.match(rule.match);
    if (m) return { text: rule.text(m), fix: rule.fix?.(m, ids) };
  }
  return { text: diagnostic };
}
