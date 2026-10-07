// "Open in…" (ADR-0052 §3.4): from any record or object type, the other places
// the platform shows the same thing — its records in the Object Explorer, its
// definition in the Ontology, the pages composed over it, its lineage. Only
// routes to views this member's applications contribute are offered; the
// workspace reports a missing view, never a forbidden one.
import { ActionMenu, t, useWorkspace, type ContextCommand } from "@platform/ui";
import { assetKey, useHost } from "../index";

export type OpenInPlace = "explorer" | "ontology" | "pages" | "lineage";

export function OpenIn({ type, exclude = [] }: { type: string; exclude?: OpenInPlace[] }) {
  const { definitions, role } = useHost();
  const { open } = useWorkspace();
  const definition = definitions.find((d) => d.entity?.type === type);
  const builder = role("build") === "builder";
  const commands: ContextCommand[] = [];
  if (!exclude.includes("explorer")) commands.push({ id: "explorer", label: t("Object Explorer"), run: () => open({ view: "explorer", params: { type } }) });
  if (!exclude.includes("ontology") && builder && definition?.ref.app === "build") {
    commands.push({ id: "ontology", label: t("Object type in Ontology"), run: () => open({ view: "object-type", params: { object: definition.ref.name } }) });
  }
  if (!exclude.includes("pages") && definition) {
    const key = assetKey(definition.ref);
    for (const page of definitions.filter((d) => d.page && assetKey(d.page.object) === key)) {
      commands.push({ id: `page:${assetKey(page.ref)}`, label: t("Page: {title}", { title: page.page!.title }),
        run: () => open({ view: "page", params: { app: page.ref.app, kind: page.ref.kind, name: page.ref.name } }) });
    }
  }
  if (!exclude.includes("lineage") && builder && definition) commands.push({ id: "lineage", label: t("Lineage"), separatorBefore: true, run: () => open({ view: "lineage", params: { ref: assetKey(definition.ref) } }) });
  return <ActionMenu label={t("Open in…")} commands={commands}>{t("Open in…")}</ActionMenu>;
}
