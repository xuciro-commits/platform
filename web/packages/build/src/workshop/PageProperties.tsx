import { recordComparisonSource } from "./page-editor/record-comparison";
import { CompositeBindingInspector } from "./page-editor/CompositeBindingInspector";
import { ModelBindingInspector } from "./page-editor/ModelBindingInspector";
import { useAuthoringModel } from "../ontology/authoring-model";
import { collaborationRecordSource, requiresOriginalRecord } from "./page-editor/collaboration";
import { interfaceQueryForSection } from "@platform/app";
import { recordPaths } from "../ontology/record-paths";
import { SemanticObjectSelect, pageVariableContract, widgetContract, useHost, useProvidedCapabilities } from "@platform/app";
import { Button, Checkbox, InspectorSection, type WorkbenchTab, Input, MarkdownEditor, Select, Textarea, Toggles, t, type EntityInfo } from "@platform/ui";
import { Plus, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import type { Api as HostApi } from "@platform/kernel";
import { BindingEditor } from "../automate/workflow-binding";
import { variableAccessible, overlayOwner, loopOwner } from "./page-layout";
import { widgetBindingInspector } from "./page-editor/widgets/registry";
import { InspectorFrame } from "./page-editor/widgets/InspectorFrame";
import type { PageUI } from "@platform/app";
import {filterable} from "./editor";
import type {Draft} from "./editor";

type PropertiesProps = {
  section?: Draft; sections:Draft[]; document: PageUI.PageDocument; info?: EntityInfo; object: string; relatedObjects?: string[];
  selections: HostApi.SelectionVariable[];
  catalog: { schema: string; title: string; target: string; draft?: boolean }[];
  pageActions: string[];
  onChange: (patch: Partial<Draft>) => void;
  onResourcesChange:(patch:Partial<Draft>,variables:Record<string,HostApi.PageVariable>)=>void;
};

export function widgetInspectorTabs(props: PropertiesProps & { section: Draft; events: ReactNode; appearance: ReactNode }): WorkbenchTab[] {
  const { section, sections, document, info, object, onChange, onResourcesChange } = props;
  const leaf = Object.entries(document.nodes).find(([, node]) => node.section === section.id)?.[0];
  const overlay = leaf ? overlayOwner(document, leaf) : undefined;
  const Inspector = widgetBindingInspector(section.widget, section.configVersion ?? 0);
  const contract = widgetContract(section.widget);
  return [
    { id: "properties", title: t("Properties"), content: <div className="grid content-start">
      <InspectorSection title={t("General")}>
        <label className="grid gap-1 text-xs">{t("Title")}<Input value={section.title ?? ""} onChange={(event) => onChange({ title: event.target.value })} /></label>
        {section.widget === "input" && <label className="grid gap-1 text-xs">{t("Input presentation")}
          <Select value={section.inputKind ?? ""} onChange={(event) => onChange({ inputKind: event.target.value || undefined })}>
            <option value="">{t("Text input")}</option><option value="search">{t("Scoped record search")}</option><option value="scan">{t("Barcode scan (search by scanner or camera)")}</option>
          </Select>
        </label>}
        {section.widget === "text" && <div className="grid gap-1 text-xs">
          <span className="font-medium text-muted">{t("Words")}</span>
          <MarkdownEditor value={section.text ?? ""} onChange={(text) => onChange({ text })} rows={6} placeholder={t("Write markdown here…")} />
        </div>}
      </InspectorSection>
      {Inspector && <InspectorSection title={t("Widget settings")}>
        <InspectorFrame id={section.id ?? "selected"} widget={section.widget} version={section.configVersion ?? 0} part="bindings">
          <Inspector section={section} document={document} object={object} info={info} overlay={overlay}
            itemOwner={leaf ? loopOwner(document, leaf) : undefined} sections={sections}
            onResourcesChange={onResourcesChange} onChange={onChange} />
        </InspectorFrame>
      </InspectorSection>}
    </div> },
    { id: "data", title: t("Data"), content: <div className="grid content-start"><Properties {...props} /></div> },
    ...(contract && "events" in contract && contract.events.length ? [{ id: "events", title: t("Events"), badge: document.events?.filter((binding) => binding.source === section.id).length || undefined, content: <div className="grid content-start gap-2 p-2">{props.events}</div> }] : []),
    { id: "appearance", title: t("Appearance"), content: <div className="grid content-start">{props.appearance}</div> },
  ];
}

function Properties({ section, sections, document, info, catalog, pageActions, object, selections, relatedObjects = [], onChange }: PropertiesProps) {
  const { definitions } = useHost();
  const model = useAuthoringModel();
  const capabilityQuery = useProvidedCapabilities();
  const provided = capabilityQuery.data ?? [];
  const computations = provided.filter(item => item.ref.kind === "compute" && item.capability.kind === "operation" && item.capability.side === "pure").map(item => ({
    binding: { ref: item.ref, sourceVersion: item.version }, name: item.ref.name,
    title: definitions.find(definition => definition.ref.app === item.ref.app && definition.ref.name === item.ref.name && definition.ref.kind === "compute")?.operation?.title ?? item.capability.name,
    input: item.capability.inputs,
  }));
  const functions = provided.filter(item => item.ref.app === "build" && item.capability.kind === "function").flatMap(item => {
    const definition = definitions.find(definition => definition.ref.app === item.ref.app && definition.ref.kind === "function" && definition.ref.name === item.ref.name && definition.version === item.version);
    return definition?.function?.object === object ? [{ name: item.ref.name, title: definition.function.title, version:item.version,binding:{ref:item.ref,sourceVersion:item.version} }] : [];
  });
  const queriesOf = (obj: string) => definitions
    .filter(definition => definition.query?.object === obj && provided.some(item => item.capability.kind === "query" && item.capability.side === "pure" && item.ref.app === definition.ref.app && item.ref.name === definition.ref.name && item.version === definition.version))
    .map(definition => ({ key: `${definition.ref.app}.${definition.ref.name}`, title: definition.query?.title ?? definition.ref.name,version:definition.version }));
  if (!section) return <p className="p-3 text-xs text-muted">{t("Choose a section to configure it.")}</p>;
  const contract = widgetContract(section.widget);
  const allows = (kind: string) => contract?.bindingKinds.some((binding) => binding === kind) ?? false;
  const fields = info?.fields ?? [];
  const parentType = section.parentSelection ? selections.find((selection) => selection.name === section.parentSelection)?.object.name ?? "" : object;
  const relations = fields.filter((field) => field.type === "reference" && field.ref === parentType && field.inverse).map((field) => field.inverse!);
  const actions = catalog.filter(action => action.target === (section.object || object) && (action.draft || provided.some(item => item.capability.kind === "action" && item.capability.side === "authoritative" && item.capability.name === action.schema)));
  const listActions = ["table", "record-list"].includes(section.widget);
  const leaf=Object.entries(document.nodes).find(([,n])=>n.section===section.id)?.[0],overlay=leaf?overlayOwner(document,leaf):undefined;
  const accessible=(v:HostApi.PageVariable)=>variableAccessible(v,undefined,overlay);
  const measures = ["count", ...fields.filter((f) => f.type === "integer" || f.type === "decimal" || f.type === "money").flatMap((f) => [`sum:${f.name}`, `avg:${f.name}`])];
  return (
    <InspectorSection title={t("Data bindings")}>
      {capabilityQuery.isError && <p role="alert" className="text-xs text-danger">{t("Available capabilities could not be loaded.")}</p>}
      {allows("composite")&&<CompositeBindingInspector section={section} sections={sections} document={document} object={object} selections={selections} lookup={model.entity} definitions={definitions} onChange={onChange}/>}
      {allows("object")&&<ModelBindingInspector section={section} sections={sections} document={document} object={object} selections={selections} lookup={type=>type===info?.type?info:model.entity(type)} definitions={definitions} onChange={onChange}/>}
      {!section.model&&relatedObjects.length > 0 && allows("object") && !interfaceQueryForSection({document,sections},section) && (
        <label className="grid gap-1 text-xs">{t("Object")}
          <SemanticObjectSelect label={t("Object")} value={section.object ?? object} drafts={model.drafts.data?.records.filter(draft => !draft.archived && (`build.${draft.name}` === object || relatedObjects.includes(`build.${draft.name}`))).map(draft => ({ name: `build.${draft.name}`, title: draft.title }))} filter={(definition) => definition.ref.name === object || relatedObjects.includes(definition.ref.name)}
            onChange={(ref) => { if (ref) onChange({ object: ref.name === object ? undefined : ref.name,
              selection: undefined,countVariable:undefined,statusTracker:undefined,recordLinks:undefined,tableColumns:undefined, collectionVariable:undefined, parentSelection: undefined, relation: undefined, query: undefined, inputs: undefined, timeStart:undefined,timeEnd:undefined,timeLabel:section.widget==="record-timeline"?"id":undefined,timeGroup:undefined,cardLabel:section.widget==="kanban"?"id":undefined, fields: [], actions: [] }); }} />
        </label>
      )}
      {allows("filter-variable")&&!(leaf&&loopOwner(document,leaf))&&(!overlay||section.widget!=="filter")&&!section.collectionVariable&&<label className="grid gap-1 text-xs">{t("Shared filter binding")}<Select value={section.filterVariable??""} onChange={(e)=>onChange({filterVariable:e.target.value||undefined})}><option value="">{t("Keep filters in this page")}</option>{Object.entries(document.variables??{}).filter(([,v])=>v.mode==="shared"&&v.type==="filter"&&v.source?.object?.name===(section.object||object)&&(section.widget!=="filter"||v.writable)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
      {section.widget==="metric" && <label className="grid gap-1 text-xs">{t("Aggregate query set")}<Select value={section.collectionVariable??""} onChange={(event)=>{const variable=document.variables?.[event.target.value],query=variable?.source?.query?document.queries?.[variable.source.query]:undefined,shared=variable?.source?.object;onChange({collectionVariable:event.target.value||undefined,filterVariable:undefined,query:undefined,parentSelection:undefined,relation:undefined,...((query?.object||shared)?{object:(query?.object||shared)!.name===object?undefined:(query?.object||shared)!.name}:{} )});}}><option value="">{t("Use the widget's own aggregate")}</option>{Object.entries(document.variables??{}).filter(([,v])=>accessible(v)&&v.type==="object-set"&&(v.source?.kind==="plan"||v.mode==="shared"&&!!v.source?.object)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
      {allows("record-set-variable")&&<label className="grid gap-1 text-xs">{t("Input record set binding")}<Select value={section.recordSetVariable??""} onChange={e=>{const binding=recordComparisonSource(document,sections,e.target.value,section.id??"",object),changed=binding&&binding.object!==(section.object||object);onChange({recordSetVariable:e.target.value||undefined,...(binding?{object:binding.object===object?undefined:binding.object}:{}),...(changed?{fields:[],recordComparison:{labelField:"id"}}:{})});}}><option value="">{t("Choose an original multi-selection")}</option>{Object.entries(document.variables??{}).filter(([id])=>!!recordComparisonSource(document,sections,id,section.id??"",object)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
      {allows("record-variable") && <label className="grid gap-1 text-xs">{t("Input record binding")}<Select value={document.variables?.[section.recordVariable ?? ""]?.source?.kind === "record" || document.variables?.[section.recordVariable ?? ""]?.mode === "input" || document.variables?.[section.recordVariable ?? ""]?.mode === "shared" ? section.recordVariable : ""} onChange={(e) => {const original=requiresOriginalRecord(section)?collaborationRecordSource(document,sections,e.target.value,section.id??"",object):undefined;onChange({recordVariable:e.target.value||undefined,selection:undefined,...(section.widget==="breadcrumb"&&!e.target.value?{object:undefined,breadcrumb:section.breadcrumb?{...section.breadcrumb,labelField:undefined}:undefined}:{}),...(original?{object:original.object===object?undefined:original.object,fields:[],actions:[]}:{})});}}><option value="">{t(requiresOriginalRecord(section)?"Choose an original record resource":"Use page selection")}</option>{!requiresOriginalRecord(section)&&Object.entries(document.interface?.inputs ?? {}).filter(([, p]) => p.type === "record").map(([id, p]) => <option key={id} value={p.variable}>{id}</option>)}{Object.entries(document.variables??{}).filter(([id,v])=>requiresOriginalRecord(section)?!!collaborationRecordSource(document,sections,id,section.id??"",object):v.type==="record"&&(v.source?.kind==="record"||v.mode==="shared")&&accessible(v)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
      {section.recordVariable && <p className="text-xs text-muted">{t(document.variables?.[section.recordVariable]?.mode === "input" ? "This widget reads the input record." : document.variables?.[section.recordVariable]?.source?.kind==="record" ? "This widget reads the bound record selection." : "This widget reads the current loop record.")}</p>}
      {!interfaceQueryForSection({document,sections},section) && !requiresOriginalRecord(section) && !section.recordVariable && (selections.length > 0 || section.selection) && contract?.selectionMode !== "none" &&
        <label className="grid gap-1 text-xs">{contract?.selectionMode === "write" ? t("Writes selection") : t("Reads selection")}
          <Select value={section.selection ?? ""} onChange={(e) => onChange({ selection: e.target.value || undefined })}>
            <option value="">{t("Shared selection for this object")}</option>
            {section.selection && !selections.some((v) => v.name === section.selection && v.object.name === (section.object || object)) &&
              <option value={section.selection}>{t("Unavailable selection: {name}", { name: section.selection })}</option>}
            {selections.filter((v) => v.object.name === (section.object || object)).map((v) => <option key={v.name} value={v.name}>{v.name}</option>)}
          </Select>
        </label>}
      {!section.model&&(selections.length > 0 || section.parentSelection) && section.object && relatedObjects.includes(section.object) &&
        !section.collectionVariable && allows("relation") &&
        <label className="grid gap-1 text-xs">{t("Parent selection")}
          <Select value={section.parentSelection ?? ""} onChange={(e) => { const nextType = e.target.value ? selections.find((selection) => selection.name === e.target.value)?.object.name : object; onChange({ parentSelection: e.target.value || undefined, ...(nextType !== parentType ? { relation: undefined, inputs: undefined } : {}) }); }}>
            <option value="">{t("Page's shared selection")}</option>
            {section.parentSelection && !selections.some((v) => v.name === section.parentSelection && fields.some((field) => field.ref === v.object.name)) &&
              <option value={section.parentSelection}>{t("Unavailable selection: {name}", { name: section.parentSelection })}</option>}
            {selections.filter((v) => fields.some((field) => field.type === "reference" && field.ref === v.object.name)).map((v) => <option key={v.name} value={v.name}>{v.name}</option>)}
          </Select>
        </label>}
      {!section.model&&section.object && relations.length > 0 && !section.collectionVariable && allows("relation") && (
        <label className="grid gap-1 text-xs">{t("Through")}
          <Select value={section.relation ?? ""} onChange={(e) => {
            const parent = fields.find((f) => f.type === "reference" && f.ref === parentType && f.inverse === e.target.value);
            onChange({ relation: e.target.value || undefined,
              ...(section.widget === "form" && parent && e.target.value ? { fields: section.fields?.filter((name) => name !== parent.name) } : {}) });
          }}>
            <option value="">{section.widget === "form" ? t("Choose the parent in the form") : t("Any reference to this page's object")}</option>
            {relations.map((name) => <option key={name} value={name}>{name}</option>)}
          </Select>
        </label>
      )}
      {!section.model&&!section.collectionVariable && allows("query") && queriesOf(section.object || object).length > 0 && (
        <label className="grid gap-1 text-xs">{t("Query")}
          <Select value={section.query ?? ""} onChange={(e) => onChange({ query: e.target.value || undefined,querySourceVersion:queriesOf(section.object||object).find(query=>query.key===e.target.value)?.version })}>
            <option value="">{t("All records it may read")}</option>
            {queriesOf(section.object || object).map((q) => <option key={q.key} value={q.key}>{q.title}</option>)}
          </Select>
        </label>
      )}
      {section.widget === "function" && <>
        <label className="grid gap-1 text-xs">{t("Published function version")}
          <Select value={section.function ? `${section.function.ref.name}:${section.function.sourceVersion}` : ""} onChange={(e) => {
            const selected = functions.find((f) => `${f.name}:${f.version}` === e.target.value);
            onChange({ function: selected?.binding });
          }}><option value="">{t("Choose a published function")}</option>
            {section.function && !functions.some(fn => fn.name === section.function!.ref.name && fn.version === section.function!.sourceVersion) && <option disabled value={`${section.function.ref.name}:${section.function.sourceVersion}`}>{t("Retained binding")}: {section.function.ref.name} · {section.function.sourceVersion}</option>}
            {functions.map((f) => <option key={`${f.name}:${f.version}`} value={`${f.name}:${f.version}`}>{f.title} · {t("Version")} {f.version}</option>)}
          </Select></label>
        <p className="text-xs text-muted">{t("The selected record supplies the function input. Its typed answer stays in a separate call record.")}</p>
      </>}
      {section.widget === "compute" && <>
        <label className="grid gap-1 text-xs">{t("Published code function version")}
          <Select value={section.operation ? `${section.operation.ref.app}/${section.operation.ref.name}:${section.operation.sourceVersion}` : ""} onChange={(event) => {
            const selected = computations.find((item) => `${item.binding.ref.app}/${item.name}:${item.binding.sourceVersion}` === event.target.value);
            onChange({ operation: selected?.binding, inputs: undefined });
          }}><option value="">{t("Choose a published code function")}</option>{section.operation && !computations.some(item => item.name === section.operation!.ref.name && item.binding.ref.app === section.operation!.ref.app && item.binding.sourceVersion === section.operation!.sourceVersion) && <option disabled value={`${section.operation.ref.app}/${section.operation.ref.name}:${section.operation.sourceVersion}`}>{t("Retained binding")}: {section.operation.ref.name} · {section.operation.sourceVersion}</option>}{computations.map((item) => <option key={`${item.binding.ref.app}/${item.name}:${item.binding.sourceVersion}`} value={`${item.binding.ref.app}/${item.name}:${item.binding.sourceVersion}`}>{item.title} · {item.binding.ref.app} · {item.binding.sourceVersion}</option>)}</Select>
        </label>
        {(() => {
          const schema = computations.find((item) => item.binding.sourceVersion === section.operation?.sourceVersion && item.name === section.operation?.ref.name && item.binding.ref.app === section.operation?.ref.app)?.input;
          if (schema?.type !== "object") return <p className="text-xs text-muted">{t("The operator supplies the complete typed input.")}</p>;
          return <><Button size="sm" onClick={() => onChange({ inputs: section.inputs ? undefined : Object.fromEntries(Object.keys(schema.properties ?? {}).map((name) => [name, { source: "input", path: [name] }])) })}>{section.inputs ? t("Use operator input") : t("Bind calculation inputs")}</Button>
            {section.inputs && Object.entries(schema.properties ?? {}).map(([name, field]) => <BindingEditor key={name} label={name} schema={field} value={section.inputs?.[name]} steps={[]} sources={["literal", "input", "subject"]} optional={!schema.required?.includes(name)}
              onChange={(value) => { const inputs = { ...section.inputs }; if (value) inputs[name] = value; else delete inputs[name]; onChange({ inputs }); }} />)}</>;
        })()}
        <p className="text-xs text-muted">{t("Record inputs are read by the host with the operator's permissions and retain their sources.")}</p>
      </>}
      {section.widget === "filter" && !section.collectionVariable && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Fields it filters by")}</legend>
          <Toggles options={fields.filter((f) => filterable.includes(f.type)).map((f) => ({ value: f.name, label: f.title }))} value={section.fields ?? []}
            onChange={(value) => onChange({ fields: value })} empty={t("This object has no choice, yes/no or reference field to filter by.")} />
          <p className="text-muted">{t("Tables, charts and metrics over the same object show only what it lets through.")}</p>
        </fieldset>
      )}
      {section.widget === "form" && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Fields it asks for")}</legend>
          <Toggles options={fields.filter((f) => !f.readOnly && !section.inputs?.[f.name] && !(section.relation && f.type === "reference" && f.ref === parentType && f.inverse === section.relation)).map((f) => ({ value: f.name, label: f.required ? `${f.title} *` : f.title }))} value={section.fields ?? []}
            onChange={(value) => onChange({ fields: value })} />
          <p className="text-muted">{section.relation
            ? t("The selected parent supplies its reference. Choose the remaining fields; creation still uses the object's own action.")
            : t("It makes a new record through the object's own create action; fields marked * are needed.")}</p>
        </fieldset>
      )}
      {section.widget === "form" && <FormInputBindings fields={fields.filter((field) => !field.readOnly && !(section.relation && field.ref === parentType && field.inverse === section.relation))}
        parentType={section.relation ? parentType : undefined} entity={model.entity} inputs={section.inputs ?? {}} onChange={(inputs, field, bound) => onChange({ inputs,
          fields: bound ? section.fields?.filter((name) => name !== field) : [...new Set([...(section.fields ?? []), field])] })} />}
      {(section.widget === "timeline" || section.widget === "tasks") && (
        <p className="text-xs text-muted">{section.widget === "timeline"
          ? t("It shows what happened to the record selected in a table.")
          : t("It shows what waits on the record selected in a table, for whoever opens the page.")}</p>
      )}
      {["detail","record-view"].includes(section.widget) && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Fields it shows")}</legend>
          <Toggles options={fields.map((f) => ({ value: f.name, label: f.title }))} value={section.fields ?? []}
            onChange={(value) => onChange({ fields: value })} />
        </fieldset>
      )}
      {["actions","record-view","table","record-list"].includes(section.widget) && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Actions it offers")}</legend>
          {listActions && <Checkbox checked={section.actions == null} onChange={inherit => onChange({actions:inherit?undefined:[]})}>{t("Inherit page actions")}</Checkbox>}
          {listActions && <p className="text-muted">{t("Turn off inheritance to choose actions for this list. Choose none for a read-only list.")}</p>}
          {listActions && <p className="text-muted">{t("CSV import needs both Create and Edit.")}</p>}
          <Toggles options={(listActions?actions.filter(action=>pageActions.includes(action.schema)):actions).map((a) => ({ value: a.schema, label: a.title }))} value={listActions&&section.actions==null?pageActions:section.actions ?? []} disabled={listActions&&section.actions==null}
            onChange={(value) => onChange({ actions: value })} empty={t("No action of this object is offered to you.")} />
        </fieldset>
      )}
      {section.widget === "metric" && (
        <label className="grid gap-1 text-xs">{t("Measure")}
          <Select value={section.measure ?? "count"} onChange={(e) => onChange({ measure: e.target.value })}>
            {measures.map((m) => <option key={m} value={m}>{m}</option>)}
          </Select>
        </label>
      )}
    </InspectorSection>
  );
}

export function Settings({ value, object, selections, objects, onSelections, onChange, device, onDevice }: {
  value: { title: string; description: string }; object: string; device: string; onDevice: (device: string) => void;
  selections: HostApi.SelectionVariable[]; objects: HostApi.AssetRef[];
  onSelections: (next: HostApi.SelectionVariable[], rename?: { from: string; to: string }) => void;
  onChange: (patch: Partial<{ title: string; description: string }>) => void;
}) {
  return (
    <div className="grid content-start">
      <InspectorSection title={t("Page settings")}>
      <label className="grid gap-1 text-xs">{t("What people call it")}
        <Input value={value.title} onChange={(e) => onChange({ title: e.target.value })} />
      </label>
      <label className="grid gap-1 text-xs">{t("What people do on this page")}
        <Textarea rows={4} value={value.description} onChange={(e) => onChange({ description: e.target.value })} />
      </label>
      <p className="text-xs text-muted">{t("It shows {object}. Its name and object stay as they are: applications and links name them.", { object })}</p>
      <label className="grid gap-1 text-xs">{t("Device")}
        <Select value={device} onChange={(e) => onDevice(e.target.value)}>
          <option value="">{t("Desk: columns as laid out")}</option>
          {pageVariableContract.device.kinds.map((kind) => <option key={kind} value={kind}>{t(kind === "handheld" ? "Handheld terminal: one column, large targets" : kind)}</option>)}
        </Select>
      </label>
      </InspectorSection>
      <InspectorSection title={t("Record selections")}>
        <p className="text-xs text-muted">{t("A table writes a selection; details and actions read it. Each selection holds records of one object.")}</p>
        {selections.map((v, i) => <div key={i} className="grid gap-2 rounded-sm border border-border p-2">
          <label className="grid gap-1 text-xs">{t("Selection name")}
            <Input value={v.name} onChange={(e) => { onSelections(selections.map((row, at) => at === i ? { ...row, name: e.target.value } : row), { from: v.name, to: e.target.value }); }} />
          </label>
          <label className="grid gap-1 text-xs">{t("Selection object")}
            <Select value={v.object.name} onChange={(e) => {
              const object = objects.find((ref) => ref.name === e.target.value);
              if (object) onSelections(selections.map((row, at) => at === i ? { ...row, object } : row));
            }}>{objects.map((ref) => <option key={ref.name} value={ref.name}>{ref.name}</option>)}</Select>
          </label>
          <Button size="sm" variant="ghost" onClick={() => onSelections(selections.filter((_, at) => at !== i))}><Trash2 />{t("Remove selection")}</Button>
        </div>)}
        <Button size="sm" disabled={!objects.length} onClick={() => {
          let n = 1; while (selections.some((v) => v.name === `selection${n}`)) n++;
          onSelections([...selections, { name: `selection${n}`, object: objects[0]! }]);
        }}><Plus />{t("Add record selection")}</Button>
      </InspectorSection>
    </div>
  );
}

function FormInputBindings({ fields, parentType, entity, inputs, onChange }: {
  fields: EntityInfo["fields"]; parentType?: string; entity: (type: string) => EntityInfo | undefined;
  inputs: Record<string, HostApi.Binding>; onChange: (inputs: Record<string, HostApi.Binding>, field: string, bound: boolean) => void;
}) {
  const paths = (target: EntityInfo["fields"][number]) => parentType ? recordPaths(parentType, entity).filter(({ field }) =>
    (field.type === target.type || field.type === "integer" && target.type === "decimal") && (field.type !== "reference" || field.ref === target.ref)) : [];
  const update = (field: string, value?: HostApi.Binding) => {
    const next = { ...inputs }; if (value) next[field] = value; else delete next[field];
    onChange(next, field, !!value);
  };
  return <fieldset className="grid gap-2 text-xs"><legend className="mb-1">{t("Supplied form inputs")}</legend>
    {fields.map((field) => {
      const options = paths(field), binding = inputs[field.name];
      const choice = binding?.source === "subject" ? `path:${binding.path?.join(".")}` : binding ? "literal" : "manual";
      const schema: HostApi.ValueSchema = { type: field.type === "integer" ? "integer" : field.type === "decimal" ? "number" : field.type === "boolean" ? "boolean" : "string", ...(field.choices?.length ? { enum: field.choices } : {}) };
      return <div key={field.name} className="grid gap-1"><label className="grid gap-1">{field.title}
        <Select aria-label={t("Input source for {field}", { field: field.title })} value={choice} onChange={(event) => {
          const value = event.target.value;
          update(field.name, value === "manual" ? undefined : value === "literal" ? { source: "literal", value: schema.type === "integer" || schema.type === "number" ? 0 : schema.type === "boolean" ? false : "" } : { source: "subject", path: value.slice(5).split(".") });
        }}><option value="manual">{t("Operator input")}</option><option value="literal">{t("Constant")}</option>
          {binding?.source === "subject" && !options.some((option) => `path:${option.path.join(".")}` === choice) && <option value={choice}>{t("Unavailable record path")}</option>}
          {options.map((option) => <option key={option.path.join(".")} value={`path:${option.path.join(".")}`}>{option.label}</option>)}
        </Select></label>
        {binding?.source === "literal" && <BindingEditor label={field.title} value={binding} schema={schema} sources={["literal"]} steps={[]} onChange={(value) => update(field.name, value)} />}
      </div>;
    })}
    <p className="text-muted">{t("Bound fields are read-only here. The host reads record paths with the operator's permissions when creating the record.")}</p>
  </fieldset>;
}
