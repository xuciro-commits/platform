import type { Api } from "@platform/api";
import { useHost } from "@platform/app";
import { useEffect } from "react";
import { t } from "@platform/ui";
import { useApplicationWorkspace } from "../projects/application-scope";
import { useDefinitionDraft } from "../session/definition-draft";
import { useAuthoringModel } from "./authoring-model";
import { actionIssues, nameOf, type Action, type Process } from "./object-model";

export type ActionTypeRecord = {
  id: string; revision: number; name: string; title: string; object: string;
  definition: Action; commands?: Api.DispatchCommand[]; version: number;
  state?: string; published?: string; versions?: { version: number; definition: Action; commands?: Api.DispatchCommand[] }[]; archived?: boolean;
};

export function useActionTypeDraft(id: string, initialObject?: string) {
  const { source } = useHost(), { open } = useApplicationWorkspace();
  const model = useAuthoringModel();
  const editor = useDefinitionDraft<ActionTypeRecord>({ id, type: "build.actiontype", view: "action-type",
    empty: () => ({ id: "", revision: 0, name: "", title: "", object: initialObject ?? "", version: 0,
      definition: { name: "action", title: t("New action"), from: [] } }),
    payload: value => ({ name: value.name, object: value.object, definition: value.definition, commands: value.commands }),
    failure: t("The action request could not be completed. Your draft is still here."),
  });
  const record = editor.draft;
  const object = model.drafts.data?.records.find(value => `build.${value.name}` === record.object);
  const process: Process = { ...object, fields: object?.fields ?? [], states: object?.states ?? [], access: object?.access ?? [],
    actions: [...(object?.actions ?? []).filter(value => value.name !== record.definition.name), record.definition],
    dispatchCommands: record.commands ?? object?.dispatchCommands,
  };
  const change = (next: Process) => {
    const definition = next.actions[process.actions.length - 1]!;
    editor.patch({ definition, name: `${record.object}.${definition.name}`,
      ...(next.dispatchCommands !== process.dispatchCommands ? { commands: next.dispatchCommands } : {}),
    });
  };
  const roles = process.access.filter(value => value.read !== "none").map(value => value.role);
  const selectObject = (type: string) => {
    const target = model.drafts.data?.records.find(value => `build.${value.name}` === type);
    const name = nameOf(record.definition.name, target?.actions?.map(action => action.name) ?? []);
    editor.patch({ object: type, name: `${type}.${name}`,
      definition: { ...record.definition, name, from: target?.states?.[0] ? [target.states[0].name] : [],
        transaction: { subject: type, steps: [{ name: "update", kind: "update", target: { type, id: "$subject" }, changes: [] }] },
      },
    });
  };
  useEffect(() => {
    if (id === "new" && initialObject && object && !editor.dirty) selectObject(initialObject);
  }, [id, initialObject, object?.id, editor.dirty]);
  return { ...editor, record, object, process, change, open, parent: record.object,
    objects: model.drafts.data?.records ?? [], entities: model.entities, roles,
    approverRoles: ["builder", ...process.access.map(value => value.role)],
    issues: actionIssues(record.definition, process, record.object, [], model.entities),
    definitionFields: source.entity("build.object")?.fields.find(field => field.name === "actions")?.fields,
    selectObject,
  };
}
