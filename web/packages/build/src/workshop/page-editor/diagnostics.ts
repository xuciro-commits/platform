import {pageUIManifest} from "@platform/app/page-contract";
import type {PageUI} from "@platform/app";
import type { Api, ActionDeclaration } from "@platform/kernel";
import { pageLayoutDiagnostics, pageVariableContract, pageVariableDiagnostics, parsePageDecimal, widgetContract } from "@platform/app";
import { searchInputObjects } from "@platform/app/search";
import { validActionDefaults } from "@platform/app/action-defaults";
import { validExternalFrame } from "@platform/ui/external-frame";
import { validTimestampOffset, validChoiceInput, rangeGrid, gaugeModel, progressRatio, ganttRange, type EntityInfo } from "@platform/ui";

import { materializeModelSection } from "@platform/app/model";
import { variableAccessible, overlayOwner, loopOwner } from "../page-layout";
import { pageCompatibility } from "./compatibility";
import { observationProblem } from "./observation";
import { recordWorkProblem } from "./record-work";
import { collectionAnalysisProblem } from "./collection-analysis";
import { explorationViewProblem } from "./exploration-views";
import { contextViewProblem } from "./context";
import { collaborationRecordSource, recordInputOwner, historyViewProblem, isCollaborationWidget } from "./collaboration";
import { recordComparisonSource } from "./record-comparison";
import type { PageDraft } from "./draft";

export type PageDiagnostic = {
  id: string; code: string; values?: Record<string, number>; subject?: string;
  target: { kind: "node" | "widget"; id: string } | { kind: "page" | "variables" | "queries" | "compatibility" };
};
type Context = { object: string; entity: (type: string) => EntityInfo | undefined; catalog: ActionDeclaration[]; definitions: Api.Definition[] };

/** One result owns the editor's explanations, locations and save eligibility.
 * Existing runtime and widget diagnostics retain their original semantics. */
export function pageDraftDiagnostics(draft: PageDraft, { object, entity: authoringEntity, catalog, definitions }: Context): PageDiagnostic[] {
  const { document, sections, selections } = draft;
  const issues: PageDiagnostic[] = [];
  const add = (code: string, target: PageDiagnostic["target"], subject?: string, values?: Record<string, number>) => issues.push({ id: `diagnostic:${issues.length}`, code, target, subject, values });
  const bytes = (value: string) => new TextEncoder().encode(value).length;
  const nodes = Object.entries(document.nodes);
  const leaves = new Map<string | undefined, [string, PageUI.PageLayoutNode][]>();
  for (const entry of nodes) if (entry[1].kind === "widget") {
    const previous = leaves.get(entry[1].section) ?? [];
    previous.push(entry); leaves.set(entry[1].section, previous);
  }
  for (const issue of pageLayoutDiagnostics(document, sections)) add(issue.code, { kind: "node", id: issue.node }, issue.node);
  for (const issue of pageVariableDiagnostics(document.variables ?? {})) add(issue.code, { kind: "variables" }, issue.variable);
  const compatibility = pageCompatibility(draft);
  if (!compatibility.supported || compatibility.issues.length) add("This draft needs a newer workspace version. Its saved content has been preserved.", { kind: "compatibility" });
  if (!compatibility.current.valid || Object.values(document.queries ?? {}).some(query => query.limit < 1 || query.limit > pageVariableContract.query.maxLimit || query.conditions?.some(condition => !condition.field))) add("Each query needs a field for every condition and a limit within the budget.", { kind: "queries" });
  const selectionNames = new Map<string, PageUI.SelectionVariable>();
  let invalidSelection = false;
  for (const selection of selections) {
    invalidSelection ||= !/^[a-z][a-z0-9_-]{0,63}$/.test(selection.name) || selectionNames.has(selection.name);
    selectionNames.set(selection.name, selection);
  }
  if (invalidSelection) add("Selection names must be unique lowercase identifiers.", { kind: "page" });
  for (const [id, node] of nodes) if (node.kind === "loop" && (!node.loop || !document.variables?.[node.loop.collection])) add("Choose a query window for each loop before saving.", { kind: "node", id }, id);
  for (const overlay of Object.values(document.overlays ?? {})) if (!document.nodes[overlay.root]?.children?.length && !document.unusedWidgets?.some(entry => entry.parent === overlay.root) || !overlay.title.trim()) add("Add content to each overlay and bind every button before saving.", { kind: "node", id: overlay.root }, overlay.title);

  const presentationLimits = pageVariableContract.tablePresentation;
  for (const s of sections) {
    const leaf = leaves.get(s.id)?.[0]?.[0], owner = leaf ? overlayOwner(document, leaf) : undefined, loop = leaf ? loopOwner(document, leaf) : undefined;
    const variable = (id?: string) => document.variables?.[id ?? ""];
    const collection = variable(s.collectionVariable), query = document.queries?.[collection?.source?.query ?? ""];
    const ownScope = (value?: Api.PageVariable) => !!value && !!leaf && !loop && ["page", "overlay"].includes(value.scope) && variableAccessible(value, undefined, owner);
    const check = (invalid: unknown, code: string, values?: Record<string, number>) => {
      if (invalid) add(code, s.id ? { kind: "widget", id: s.id } : { kind: "page" }, s.title || s.id, values);
    };
    if (!invalidSelection) check(s.selection && selectionNames.get(s.selection)?.object.name !== (s.object || object) || s.parentSelection && !selectionNames.has(s.parentSelection), "A widget references a missing selection or the wrong object type.");
    const contract = widgetContract(s.widget);
    check(contract && "events" in contract && contract.events.some(event => event.required && !document.events?.some(binding => binding.source === s.id && binding.event === event.id)), "Add content to each overlay and bind every button before saving.");
    check(s.widget==="button-group"&&(!(s.buttons?.length)||s.buttons.length>pageVariableContract.buttonGroup.maxButtons||s.buttons.some((b,i)=>!b.title||bytes(b.title)>pageVariableContract.buttonGroup.maxTitleBytes||s.buttons?.some((other,j)=>i!==j&&b.id===other.id)||!document.events?.some(e=>e.source===s.id&&e.control===b.id&&e.event==="click"))), "Give every group button a bounded title and its own click binding.");
    check(s.widget==="inline-action"&&(s.actions?.length!==1||!!s.actionDefaults?.length&&!validActionDefaults(authoringEntity(s.object||object||""),catalog.find(a=>a.schema===s.actions?.[0]),s.actionDefaults)), "An inline action needs exactly one action and valid defaults.");
    const displayedFields = s.model ? materializeModelSection(s, authoringEntity, true).fields : s.fields;
    check(s.inlineEdit&&(!s.inlineEdit.action||s.inlineEdit.fields.length===0||s.inlineEdit.fields.length>pageVariableContract.tableEditing.maxFields||s.inlineEdit.fields.some(f=>!displayedFields?.includes(f))), "Inline table editing needs an action and 1\u2013{n} visible fields.", { n: pageVariableContract.tableEditing.maxFields });
    check(s.widget==="record-view"&&s.recordView?.tabs.length===0, "Choose at least one record tab before saving.");
    check(s.widget==="record-links"&&(!s.recordLinks?.length||s.recordLinks.length>pageVariableContract.recordLinks.maxGroups||s.recordLinks.some((g,i)=>!g.field||!g.object.name||bytes(g.title??"")>pageVariableContract.recordLinks.maxTitleBytes||s.recordLinks?.some((h,j)=>i!==j&&g.object.name===h.object.name&&g.field===h.field))), "Choose valid related groups before saving.");
    if (s.widget === "status-tracker") {
      const value = s.statusTracker, info = authoringEntity(s.object || object), lifecycle = info?.lifecycle;
      check(!value || !lifecycle || value.field !== lifecycle.field || !info?.fields.some(field => field.name === value.field) || !value.stages.length || value.stages.length > pageVariableContract.statusTracker.maxStages || new Set(value.stages).size !== value.stages.length || value.stages.some(name => !lifecycle.states.some(state => state.name === name)), "Choose an original lifecycle field and valid stages before saving.");
    }
    check(s.widget==="heading"&&(!s.text?.trim()||bytes(s.text)>pageVariableContract.titles.maxTextBytes)||s.widget==="collection-title"&&(!s.collectionVariable||!s.countVariable), "Enter bounded heading text or bind the collection and its original count before saving.");
    if (s.widget === "record-leaderboard") {
      const config = s.leaderboard;
      check(!config || !config.valueField || !config.labelField || !Number.isInteger(config.limit) || config.limit < 1 || config.limit > 32 || query?.limit !== config.limit || !!query?.offset || JSON.stringify(query?.sort) !== JSON.stringify([`${config.ascending ? "" : "-"}${config.valueField}`, "id"]), "Bind ranking fields and a matching numeric/ID sorted Top-N query before saving.");
    }
    if (s.widget === "summary-stats") {
      const statistics = variable(s.statisticsVariable);
      check(!s.summaryField || !s.statisticsVariable || statistics?.type !== "statistics" || statistics.source?.measure !== s.summaryField || statistics.source.query !== collection?.source?.query, "Bind the summary field, query and matching statistics before saving.");
    }
    if (s.widget === "record-picker" && !s.model) check(!s.recordPicker?.labelField || collection?.source?.kind !== "plan" || query?.limit !== 20 || !!query?.offset || JSON.stringify(query?.sort) !== JSON.stringify(["id"]), "Bind a picker title and a 20-record ID-sorted query before saving.");
    check(s.widget==="spacer"&&(!s.spacer||typeof s.spacer.size!=="number"||!Number.isFinite(s.spacer.size)||s.spacer.size<0||s.spacer.size>pageUIManifest.layout.maxSize), "Declare a finite nonnegative spacer size within the layout budget before saving.");
    check(s.widget==="separator"&&(!s.separator||bytes(s.separator.label??"")>pageVariableContract.separator.maxLabelBytes), "Declare a bounded plain separator label or leave it absent before saving.");
    check(s.widget==="notice"&&(!s.notice||!(pageVariableContract.notice.tones as readonly string[]).includes(s.notice.tone)||bytes(s.notice.title??"")>pageVariableContract.notice.maxTitleBytes||bytes(s.notice.message)>pageVariableContract.notice.maxMessageBytes), "Choose a supported notice tone and bounded plain title and text before saving.");
    if (s.widget === "range-input") {
      const minimum = variable(s.rangeMinVariable), maximum = variable(s.rangeMaxVariable), config = s.rangeInput;
      check(!config || !rangeGrid(config.min, config.max, config.step) || !minimum || !maximum || s.rangeMinVariable === s.rangeMaxVariable || minimum.type !== "string" || maximum.type !== "string" || minimum.mode !== "state" || maximum.mode !== "state" || minimum.scope !== maximum.scope || minimum.owner !== maximum.owner || !["page", "overlay"].includes(minimum.scope), "Bind distinct text states with one owner and a valid decimal range before saving.");
    }
    check(s.widget==="gauge"&&(!s.gaugeValueVariable||document.variables?.[s.gaugeValueVariable]?.type!=="number"||!s.gauge||!gaugeModel(0,s.gauge.max,s.gauge.warnAt)), "Bind a number variable and positive gauge maximum before saving.");
    check(s.widget === "progress" && (!s.progressValueVariable || variable(s.progressValueVariable)?.type !== "decimal" || (s.progressTotalVariable ? variable(s.progressTotalVariable)?.type !== "decimal" || !!s.progressTotal : !progressRatio("0", s.progressTotal ?? ""))), "Bind a decimal numerator and exactly one positive fixed total or decimal denominator before saving.");
    check(s.widget==="record-gantt"&&(!s.recordGantt?.startField||!s.recordGantt?.endField||!s.recordGantt?.titleField||!s.recordGantt?.statusField||!ganttRange(s.recordGantt?.rangeStart??"",s.recordGantt?.rangeEnd??"")||!query?.sort?.length), "Bind Gantt fields, a valid fixed range and an explicitly sorted plan before saving.");
    check(s.widget==="record-calendar"&&(!s.recordCalendar?.dateField||!s.recordCalendar?.labelField||!/^\d{4}-(0[1-9]|1[0-2])$/.test(s.recordCalendar?.initialMonth??"")||s.recordCalendar?.initialMonth.startsWith("0000")||!query?.sort?.length), "Bind calendar fields, an initial month and an explicitly sorted plan before saving.");
    check(s.widget==="record-events"&&(!s.recordEvents?.timeField||!s.recordEvents?.titleField||!s.recordEvents?.severityField||!query?.sort?.length), "Bind event fields and an explicitly sorted query plan before saving.");
    check(explorationViewProblem(document,sections,s,object,type=>authoringEntity(type),definitions), "Exploration views require original typed resources, published assets, retained relations and distinct object-specific output ports.");
    check(contextViewProblem(document,sections,s,object,type=>authoringEntity(type),definitions), "Context views need their original navigation, record or personnel-query bindings; image URL, caption and height must satisfy the static image profile.");
    if (["approval-inbox", "notification-feed"].includes(s.widget)) check(!leaf || !!loop || !!s.object || !!s.fields?.length || !!s.actions?.length || !!s.recordVariable || !!s.selection || !!s.collectionVariable || !!s.selectionVariable || !!s.selectionSetVariable || !!s.filterVariable || !!s.recordSetVariable || !!s.parentSelection || !!s.relation || !!s.query || !!s.inlineEdit || !!s.commentDraftVariable || !!s.fileVariable || !!s.pdfPageVariable, "Work views use their original member services. Bounded history needs an original same-owner record resource and a window of 1–100 entries.");
    check(historyViewProblem(document,sections,s,object), "Work views use their original member services. Bounded history needs an original same-owner record resource and a window of 1\u2013100 entries.");
    if (isCollaborationWidget(s.widget)) {
      const original = collaborationRecordSource(document, sections, s.recordVariable ?? "", s.id ?? "", object), record = variable(s.recordVariable), bindingOwner = recordInputOwner(document, s);
      const value = variable(s.widget === "record-comments" ? s.commentDraftVariable : s.fileVariable), pdf = variable(s.pdfPageVariable), preview = ["media-preview", "pdf-viewer", "image-annotation", "scene-3d"].includes(s.widget);
      check(!original || (s.object || object) !== original.object || !!s.fields?.length || !!s.actions?.length || !!s.collectionVariable || !!s.selection || !!s.selectionVariable || !!s.selectionSetVariable || !!s.recordSetVariable || !!s.relation || !!s.parentSelection || !!s.query || !!s.inlineEdit || s.widget === "record-comments" && !!(s.fileVariable || s.pdfPageVariable) || s.widget === "record-uploader" && !!(s.commentDraftVariable || s.pdfPageVariable) || preview && !!s.commentDraftVariable || s.widget === "media-preview" && !!s.pdfPageVariable || !value || value.type !== "string" || value.scope !== bindingOwner?.scope || value.owner !== bindingOwner?.owner || value.mode !== "state" && (!preview || value.mode !== "constant") || s.widget === "pdf-viewer" && (!pdf || pdf.type !== "string" || pdf.mode !== "state" || pdf.scope !== record?.scope || pdf.owner !== record?.owner || s.fileVariable === s.pdfPageVariable), "Bind the original record resource and collaboration state in the same page or overlay before saving.");
    }
    if (s.widget === "record-comparison") {
      const binding = recordComparisonSource(document, sections, s.recordSetVariable ?? "", s.id ?? "", object), info = binding && authoringEntity(binding.object), label = s.recordComparison?.labelField, fields = s.fields ?? [];
      check(!binding || (s.object || object) !== binding.object || !label || label !== "id" && !info?.fields.some(field => field.name === label && ["text", "longtext", "choice", "reference"].includes(field.type)) || !fields.length || fields.length > 64 || new Set(fields).size !== fields.length || fields.some(name => !info?.fields.some(field => field.name === name && ["text", "longtext", "choice", "reference", "integer", "decimal", "money", "date", "datetime", "boolean"].includes(field.type))), "Bind an original multi-selection resource, title and 1–64 comparison fields before saving.");
    }
    if (s.widget === "record-card") {
      const record = variable(s.recordVariable);
      check(!s.recordCard?.labelField || !s.recordCard.tone || (s.fields?.length ?? 0) > 4 || !(record?.mode === "resource" && record.source?.kind === "record" || Number(document.uiProfile.split(".").at(-1)) >= 98 && record?.mode === "shared" && record.type === "record" && record.source?.object?.name === (s.object || object)), "Bind an original record resource, title and at most four card properties before saving.");
    }
    if (s.widget === "sparkline-kpi") {
      const config = s.sparkline;
      check(!config || !s.sparklineDecimalVariable && !s.sparklineNumberVariable || !!s.sparklineDecimalVariable && !!s.sparklineNumberVariable || !!s.collectionVariable && (!config.field || !query?.sort?.length || !query.limit || query.limit > 30) || !s.collectionVariable && !!config.field, "Bind one original sparkline scalar and an optional ordered 30-record numeric window before saving.");
    }
    if (s.widget === "tag-counts") {
      const output = variable(s.groupValueVariable), info = authoringEntity(s.object || object);
      check(!leaf || !!loop || collection?.scope !== (owner ? "overlay" : "page") || collection.owner !== owner || !s.group || s.group === "count" || s.group.includes(":") || !info?.fields.some(field => field.name === s.group && ["text", "choice"].includes(field.type)) || collection?.mode !== "resource" || collection.source?.kind !== "plan" || !!s.groupSetVariable || !!s.groupValueVariable && (!output || output.type !== "string" || output.mode !== "state" || output.scope !== collection.scope || output.owner !== collection.owner), "Bind a visible scalar tag group, original query and matching output before saving.");
    }
    check(s.widget==="treemap"&&(!s.group||s.group==="count"||s.group.includes(":")||document.variables?.[s.collectionVariable??""]?.source?.kind!=="plan"||!!s.groupValueVariable&&!!s.groupSetVariable), "Bind a visible treemap group, original query window and exclusive output type before saving.");
    if (s.widget === "heatmap") check(!s.group || !s.columnGroup || s.group === s.columnGroup || s.measure !== "count" || collection?.mode !== "resource" || collection.source?.kind !== "plan" || !!s.rowValueVariable && !!s.rowSetVariable || !!s.columnValueVariable && !!s.columnSetVariable, "Bind two distinct heatmap axes, complete count and the original query set before saving.");
    if (s.widget === "record-scatter") {
      const config = s.scatter;
      check(!config?.xField || !config.yField || !config.colorField || !config.labelField || collection?.mode !== "resource" || collection.source?.kind !== "plan" || !query?.sort?.length || !query.limit || query.limit > 100, "Bind visible scatter fields and an ordered original window of at most 100 records before saving.");
    }
    check(s.widget==="record-chart"&&(!s.recordChart?.xField||!s.recordChart?.yField||!query?.sort?.length), "Bind visible record chart axes and an explicitly sorted query plan before saving.");
    check(s.widget==="record-list"&&(!s.collectionVariable||!s.recordList||!s.cardLabel||(s.fields?.length??0)>pageVariableContract.recordList.maxFields), "Bind a record card window, title and bounded summary fields before saving.");
    const multipleOutput = variable(s.selectionSetVariable);
    if (s.selectionSetVariable && multipleOutput?.mode === "shared") check(s.widget !== "table" || !leaf || !!owner || !!loop || !multipleOutput.writable || multipleOutput.type !== "record-set" || multipleOutput.source?.object?.name !== (s.object || object), "Bind shared record-set output to a writable table selection in the main page before saving.");
    if (["record-list", "record-scatter", "record-leaderboard", "kanban", "record-map"].includes(s.widget) && s.selectionVariable) {
      const output = variable(s.selectionVariable);
      check(!!s.selection || s.recordList?.layout === "tiles" || !leaf || !!owner || !!loop || output?.type !== "record" || output.scope !== "application" || output.mode !== "shared" || !output.writable || output.source?.kind !== "application" || output.source.object?.name !== (s.object || object), "Bind a writable shared record output for the original object in the main page before saving.");
    }
    check(observationProblem(document,sections,s,object,type=>authoringEntity(type),definitions), "Bind original observation time, signals, owned windows and matching record or statistic ports before saving.");
    check(collectionAnalysisProblem(document,s,object,type=>authoringEntity(type),definitions), "Bind the original analysis fields, matching scalar or axis ports and query before saving.");
    check(recordWorkProblem(document,s,object,name=>authoringEntity(name),catalog,definitions), "Bind the original row action parameters, bounded tile/action window or note state before saving.");
    const presentation = s.tablePresentation, template = presentation?.titleTemplate;
    check(presentation && (s.widget !== "table" || !["compact", "normal"].includes(presentation.density) || typeof presentation.showToolbar !== "boolean" || template !== undefined && (bytes(template) > 256 || /[{}]/.test(template.replace("{count}", "")) || template.includes("{count}") && collection?.source?.kind !== "plan")) || (s.tableColumns?.length ?? 0) > presentationLimits.maxColumns || s.tableColumns?.some((column, at) => column.field !== "id" && !s.fields?.includes(column.field) || s.tableColumns?.some((other, index) => index !== at && other.field === column.field) || bytes(column.title ?? "") > presentationLimits.maxTitleBytes || column.width !== undefined && (!Number.isInteger(column.width) || column.width < presentationLimits.minWidth || column.width > presentationLimits.maxWidth)), "Table columns and controls need supported formats, density and bounded titles.");
    const annotation = s.metricPresentation?.annotation;
    check(annotation && (s.widget !== "metric" || !["up", "down", "flat"].includes(annotation.direction) || bytes(annotation.text) > 256) || s.metricPresentation && (bytes(s.metricPresentation.prefix ?? "") > pageVariableContract.metricPresentation.maxUnitBytes || bytes(s.metricPresentation.suffix ?? "") > pageVariableContract.metricPresentation.maxUnitBytes), "Metric units and static notes need supported directions and bounded text.");
    check(s.inputKind !== undefined && (s.widget !== "input" || !["search", "scan"].includes(s.inputKind) || !searchInputObjects(document, leaves.get(s.id)?.[0]?.[1].valueVariable).length), "Bind search text to a query search in the same scope before saving.");
    if (s.widget === "input") check((leaves.get(s.id) ?? []).some(([id, node]) => {
      const value = variable(node.valueVariable);
      return ["search", "scan"].includes(s.inputKind ?? "") && !searchInputObjects(document, node.valueVariable).length || !value || !(value.mode === "state" || value.mode === "shared" && value.writable) || !["string", "decimal"].includes(value.type) || !variableAccessible(value, loopOwner(document, id), overlayOwner(document, id));
    }), "Bind each input to a writable text or decimal state in its own scope.");
    if (s.widget === "record-picker" && s.pickerValueVariable) {
      const output = variable(s.pickerValueVariable);
      check(!output || output.type !== "string" || output.mode !== "state" || !s.model && (output.scope !== collection?.scope || output.owner !== collection?.owner), "Bind picker ID output to original text state with the same query owner before saving.");
    }
    if (s.widget === "alert-banner") {
      const config = s.alertBanner, value = variable(s.alertValueVariable);
      check(!config || parsePageDecimal(config.threshold)?.value !== config.threshold || !(pageVariableContract.alertBanner.tones as readonly string[]).includes(config.tone) || !config.message || bytes(config.message) > pageVariableContract.alertBanner.maxMessageBytes || value?.type !== "decimal" || !ownScope(value), "Bind an original decimal value and a valid threshold, tone and plain message before saving.");
    }
    if (s.widget === "date-input") {
      const value = variable(s.dateVariable);
      check(s.dateKind && !["date", "datetime"].includes(s.dateKind) || s.dateKind === "datetime" && !validTimestampOffset(s.dateOffset ?? "") || s.dateKind !== "datetime" && s.dateOffset || value?.type !== "string" || value.mode !== "state" || !ownScope(value) || bytes(s.dateLabel ?? "") > 1024, "Bind the date input to its original scoped text state before saving.");
    }
    if (s.widget === "boolean-input") {
      const value = variable(s.booleanVariable);
      check(s.booleanVariant && !(pageVariableContract.booleanInput.variants as readonly string[]).includes(s.booleanVariant) || value?.type !== "boolean" || value.mode !== "state" || !ownScope(value) || bytes(s.booleanLabel ?? "") > 1024, "Bind a boolean state with its original page or overlay owner before saving.");
    }
    if (s.widget === "choice-input") {
      const config = s.choiceInput, multiple = config?.variant === "multiple", value = variable(multiple ? s.choiceSetVariable : s.choiceVariable), indexed = ["steps", "tabs"].includes(config?.variant ?? "");
      check(!config || !validChoiceInput(config) || !!(multiple ? s.choiceVariable : s.choiceSetVariable) || value?.type !== (multiple ? "string-set" : "string") || value.mode !== "state" && (!indexed || value.mode !== "constant") || !ownScope(value) || indexed && (value?.scope !== (owner ? "overlay" : "page") || value.owner !== owner), "Bind a scoped text state and valid static choices before saving.");
    }
    check(s.widget === "external-frame" && !validExternalFrame(s.externalFrame), "Choose a valid external frame before saving.");
    check(s.widget === "embedded-page" && (!s.embedding || !/^page\.sha256\.[0-9a-f]{64}$/.test(s.embedding.contentVersion)), "Choose a published page content version before saving.");
    check(s.widget === "histogram" && (!s.histogram?.field || !Number.isInteger(s.histogram.bins) || s.histogram.bins < 1 || s.histogram.bins > 64 || collection?.source?.kind !== "plan"), "Bind a numeric histogram field, 1–64 bins and the original query before saving.");
    check(s.widget === "term-counts" && (!s.group || s.group === "count" || s.group.includes(":") || collection?.source?.kind !== "plan"), "Bind a visible term field and the original query before saving.");
  }
  return issues;
}
