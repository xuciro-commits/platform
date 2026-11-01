import { Button, Notice, RecordPage, t, type Route } from "@platform/ui";
import { useMemo, useState } from "react";
import { findDefinition, RecordDetail, useHost, useOpenRecord } from "../index";
import { RecordObservations } from "../observations/Observations";
import { ApplicationPage } from "../runtime/ApplicationRuntime";
import { ComposedPage, isComposed } from "./sections";
import { isPageDefinition, PageWorkspace } from "./pages";

type Props = { type: string; id: string; inspect?: boolean; route: Route };

export function ObjectRecordView(props: Props) {
  const { source } = useHost();
  return <ObjectRecordSurface key={JSON.stringify([source.scope, props.type, props.id, source.entity(props.type)?.objectView])} {...props} />;
}

function ObjectRecordSurface({ type, id, inspect, route }: Props) {
  const { source, definitions } = useHost();
  const openRecord = useOpenRecord();
  const [instance] = useState(() => crypto.randomUUID());
  const [selectedPage, setSelectedPage] = useState<string>();
  const view = source.entity(type)?.objectView;
  const application = view && findDefinition(definitions, view.application)?.application;
  const entry = view && findDefinition(definitions, view.page)?.page;
  const pageRef = view && { ...view.page, name: selectedPage && application?.pages.includes(selectedPage) ? selectedPage : view.page.name };
  const definition = pageRef && findDefinition(definitions, pageRef);
  const page = definition?.page;
  const currentInput = page?.document?.interface?.inputs?.currentObject;
  const pageCall = useMemo(() => ({ input: { version: page?.document?.interface?.version ?? 0, values: currentInput ? { currentObject: { object: type, id } } : {} } }), [type, id, page?.document?.interface?.version, currentInput]);
  if (!view) {
    if (!inspect) return <RecordDetail type={type} id={id} />;
    return <RecordPage source={source} type={type} id={id} recordTabs={["properties", "links", "history"]}
      onOpen={(target, record) => openRecord({ type: target, id: record.id })}
      actions={() => <><Button onClick={() => openRecord({ type, id }, { window: "tab" })}>{t("Open record")}</Button><RecordObservations type={type} id={id} /></>} />;
  }
  if (!application || !entry || !page || !pageRef || !application.pages.includes(view.page.name)) return <Notice tone="danger" message={t("This object's Workshop view is unavailable. Ask a builder to check its module and page.")} />;
  const current = entry.document?.interface?.inputs?.currentObject;
  if (entry.object?.name !== type || !current || current.type !== "record" || current.object?.name !== type) return <Notice tone="danger" message={t("This object's Workshop view does not accept its current object. Ask a builder to check the page inputs.")} />;
  return <section aria-label={page.title} className="grid min-w-0 gap-2">
    {inspect && <div className="flex justify-end"><Button onClick={() => openRecord({ type, id }, { window: "tab" })}>{t("Open record")}</Button></div>}
    <ApplicationPage pageRef={pageRef} applicationRef={view.application} instance={instance} onPage={setSelectedPage} route={route}>
      {isComposed(page) ? <ComposedPage page={page} definitionKey={JSON.stringify([source.scope, type, id, definition?.version])} pageCall={pageCall} /> : definition && isPageDefinition(definition) && <PageWorkspace definition={definition} />}
    </ApplicationPage>
  </section>;
}
