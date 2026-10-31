import { useNewRecord } from "@platform/app";
import { Button, Panel, t, useWorkspace } from "@platform/ui";
import { Check } from "lucide-react";
import type { ReactNode } from "react";
import type { OwnedResource } from "./resource-inventory";
import { resourceKinds, resourceRoute, type ResourceKindInfo } from "./resources";
import { resourceInArea, studioAreas, studioRoute } from "./studio-model";

type Step = { id: string; title: string; detail: string; done: boolean; optional?: boolean; label: string; unavailable?: boolean; run: () => void };

export function ProjectOverview({ project, owned, released, builder, disabled, onCreated, onDeliver, children }: {
  project: { id: string; description?: string; pages?: string[] }; owned: OwnedResource[]; released: boolean; builder: boolean; disabled: boolean;
  onCreated: (kind: ResourceKindInfo, target: { type: string; id: string }) => void; onDeliver: () => void; children?: ReactNode;
}) {
  const { open } = useWorkspace();
  const objectKind = resourceKinds.find(kind => kind.kind === "object")!;
  const connectionKind = resourceKinds.find(kind => kind.kind === "connection")!;
  const createObject = useNewRecord(objectKind.type, target => onCreated(objectKind, target));
  const objects = owned.filter(item => item.ref.kind === "object" && item.record);
  const first = objects[0]?.record;
  const steps: Step[] = [
    { id: "data", title: t("Connect data"), detail: t("Optional. Bring records in from a source system, or start with object types that only actions write."), optional: true,
      done: owned.some(item => ["connection", "source", "dataset", "pipeline"].includes(item.kind.kind)), label: t("New connection"), run: () => open(resourceRoute(connectionKind, "new", project.id)) },
    { id: "object", title: t("Define an object type"), detail: t("The business record: its properties, links and lifecycle states."),
      done: objects.length > 0, label: t("New object type"), unavailable: !createObject.available, run: () => createObject.take() },
    { id: "action", title: t("Add an action type"), detail: t("A governed change people make to a record."),
      done: objects.some(item => item.record?.actions?.length), label: t("Add an action"), unavailable: !first, run: () => first && open({ view: "object-type", params: { id: first.id, application: project.id, tab: "actions" } }) },
    { id: "page", title: t("Build a page in Workshop"), detail: t("Show the records and offer their actions."),
      done: !!project.pages?.length, label: t("Open Workshop"), run: () => open({ view: "module", params: { id: project.id, application: project.id } }) },
    { id: "release", title: t("Release"), detail: t("Test the application and activate a version."),
      done: released, label: t("Verify and release"), run: onDeliver },
  ];
  const next = steps.find(step => !step.done && !step.optional);
  return <div className="grid max-w-5xl content-start gap-4 p-4">
    {project.description && <p className="text-sm text-muted">{project.description}</p>}
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
      {studioAreas.map(area => <button key={area.id} type="button" onClick={() => open(studioRoute(project.id, area.id))}
        className="grid gap-0.5 rounded-md border border-border bg-surface p-3 text-left hover:bg-row-hover">
        <span className="text-xl font-semibold tabular-nums">{owned.filter(item => resourceInArea(item, area.id)).length}</span>
        <span className="text-xs text-muted">{t(area.title)}</span>
      </button>)}
    </div>
    <Panel title={t("Next steps")}>
      <ol className="grid divide-y divide-border">
        {steps.map((step, at) => <li key={step.id} className="flex items-center gap-3 py-2">
          <span className={step.done ? "grid size-5 shrink-0 place-items-center rounded-full bg-[var(--tone-success)] text-white [&_svg]:size-3" : "grid size-5 shrink-0 place-items-center rounded-full border border-border-strong text-2xs text-muted"}>{step.done ? <Check /> : at + 1}</span>
          <span className="grid min-w-0 flex-1"><span className={step.done ? "text-sm text-muted" : "text-sm font-medium"}>{step.title}</span><span className="text-xs text-muted">{step.detail}</span></span>
          {builder && !step.done && <Button size="sm" variant={step === next ? "primary" : undefined} disabled={disabled || step.unavailable} onClick={step.run}>{step.label}</Button>}
        </li>)}
      </ol>
    </Panel>
    {children}
    {createObject.dialog}
  </div>;
}
