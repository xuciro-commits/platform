import { type Api, request } from "../host";
import { t } from "../i18n";
import type { Described } from "./describe";

export type WorkbookSheet = {
  name: string;
  columns: string[];
  rows: string[][];
};

export type PlanField = {
  key: string;
  field: string;
  title: string;
  aliases: string[];
  source: "own" | "base";
  ref?: string;
  required?: boolean;
  choices?: { key: string; labels: string[] }[];
};

export type PlanLine = {
  row: number;
  cells: string[];
  baseId?: string;
  result?: Api.ImportRow;
  done?: boolean;
};

export type ObjectPlan = {
  type: string;
  baseType?: string;
  title: string;
  displayKey: string;
  fields: PlanField[];
  withId: boolean;
  run: (
    lines: PlanLine[],
    used: { name: string; index: number }[],
    preview: boolean,
    pendingRefs: Map<string, Set<string>>,
  ) => Promise<{ results: Map<number, Api.ImportRow>; baseIds: Map<number, string> }>;
};

const unreadable = "ERROR_CODE_INVALID_ARGUMENT: ";
export const plainKey = (text: string) => text.trim().toLowerCase();
const quoteCsv = (cell: string) => `"${cell.replaceAll("\"", "\"\"")}"`;

function columnIndex(cellRef: string, fallback: number): number {
  let letters = "";
  for (let i = 0; i < cellRef.length; i++) {
    const code = cellRef.charCodeAt(i);
    if (code >= 65 && code <= 90) letters += cellRef[i];
    else if (code >= 97 && code <= 122) letters += cellRef[i].toUpperCase();
    else break;
  }
  if (!letters) return fallback;
  let col = 0;
  for (let i = 0; i < letters.length; i++) col = col * 26 + (letters.charCodeAt(i) - 64);
  return col - 1;
}

function readRichText(node: Element): string {
  const runs = Array.from(node.getElementsByTagName("r"));
  if (runs.length > 0) {
    return runs.map((run) => Array.from(run.getElementsByTagName("t")).map((item) => item.textContent ?? "").join("")).join("");
  }
  return Array.from(node.getElementsByTagName("t")).map((item) => item.textContent ?? "").join("");
}

async function inflateRaw(data: Uint8Array): Promise<Uint8Array> {
  const copy = new Uint8Array(data.byteLength);
  copy.set(data);
  const stream = new Blob([copy]).stream().pipeThrough(new DecompressionStream("deflate-raw"));
  return new Uint8Array(await new Response(stream).arrayBuffer());
}

async function unzipEntries(raw: Uint8Array): Promise<Map<string, string> | null> {
  if (raw.byteLength < 22 || raw[0] !== 0x50 || raw[1] !== 0x4b || raw[2] !== 0x03 || raw[3] !== 0x04) return null;
  const view = new DataView(raw.buffer, raw.byteOffset, raw.byteLength);
  let eocd = -1;
  const minOffset = Math.max(0, raw.byteLength - 65558);
  for (let pos = raw.byteLength - 22; pos >= minOffset; pos--) {
    if (view.getUint32(pos, true) === 0x06054b50) {
      eocd = pos;
      break;
    }
  }
  if (eocd < 0) return null;
  const count = view.getUint16(eocd + 10, true);
  let dirPos = view.getUint32(eocd + 16, true);
  const decoder = new TextDecoder("utf-8");
  const out = new Map<string, string>();
  for (let i = 0; i < count && dirPos + 46 <= raw.byteLength; i++) {
    if (view.getUint32(dirPos, true) !== 0x02014b50) break;
    const method = view.getUint16(dirPos + 10, true);
    const compressedSize = view.getUint32(dirPos + 20, true);
    const nameLen = view.getUint16(dirPos + 28, true);
    const extraLen = view.getUint16(dirPos + 30, true);
    const commentLen = view.getUint16(dirPos + 32, true);
    const localOffset = view.getUint32(dirPos + 42, true);
    const name = decoder.decode(raw.subarray(dirPos + 46, dirPos + 46 + nameLen)).replace(/^\/+/, "");
    dirPos += 46 + nameLen + extraLen + commentLen;
    if (!name.endsWith(".xml") && !name.endsWith(".rels")) continue;
    if (localOffset + 30 > raw.byteLength || view.getUint32(localOffset, true) !== 0x04034b50) continue;
    const localNameLen = view.getUint16(localOffset + 26, true);
    const localExtraLen = view.getUint16(localOffset + 28, true);
    const dataStart = localOffset + 30 + localNameLen + localExtraLen;
    const slice = raw.subarray(dataStart, dataStart + compressedSize);
    const bytes = method === 0 ? slice : method === 8 ? await inflateRaw(slice) : null;
    if (bytes) out.set(name, decoder.decode(bytes));
  }
  return out;
}

export async function readWorkbookSheets(file: File): Promise<WorkbookSheet[] | null> {
  const raw = new Uint8Array(await file.arrayBuffer());
  const entries = await unzipEntries(raw);
  if (!entries) return null;
  const workbookXml = entries.get("xl/workbook.xml");
  const relsXml = entries.get("xl/_rels/workbook.xml.rels");
  if (!workbookXml || !relsXml) return null;
  const parser = new DOMParser();
  const workbookDoc = parser.parseFromString(workbookXml, "application/xml");
  const relsDoc = parser.parseFromString(relsXml, "application/xml");
  const targets = new Map<string, string>();
  for (const rel of Array.from(relsDoc.getElementsByTagName("Relationship"))) {
    const id = rel.getAttribute("Id") ?? "";
    const target = (rel.getAttribute("Target") ?? "").replace(/^\/+/, "");
    if (id && target) targets.set(id, target.startsWith("xl/") ? target : `xl/${target}`);
  }
  const shared: string[] = [];
  const sharedXml = entries.get("xl/sharedStrings.xml");
  if (sharedXml) {
    const sharedDoc = parser.parseFromString(sharedXml, "application/xml");
    for (const item of Array.from(sharedDoc.getElementsByTagName("si"))) shared.push(readRichText(item));
  }
  const sheets: WorkbookSheet[] = [];
  const relNs = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
  for (const node of Array.from(workbookDoc.getElementsByTagName("sheet"))) {
    const name = (node.getAttribute("name") ?? "").trim() || `Sheet${sheets.length + 1}`;
    const relId = node.getAttribute("r:id") || node.getAttributeNS(relNs, "id") || "";
    const sheetXml = entries.get(targets.get(relId) ?? "");
    if (!sheetXml) continue;
    const sheetDoc = parser.parseFromString(sheetXml, "application/xml");
    const parsedRows: string[][] = [];
    for (const rowNode of Array.from(sheetDoc.getElementsByTagName("row"))) {
      const cells: string[] = [];
      let cursor = 0;
      for (const cellNode of Array.from(rowNode.getElementsByTagName("c"))) {
        const idx = columnIndex(cellNode.getAttribute("r") ?? "", cursor);
        cursor = idx + 1;
        const kind = cellNode.getAttribute("t") ?? "";
        const rawVal = cellNode.getElementsByTagName("v")[0]?.textContent ?? "";
        const text = kind === "s"
          ? (shared[Number(rawVal)] ?? "")
          : kind === "inlineStr"
            ? readRichText(cellNode)
            : kind === "b"
              ? (rawVal === "1" ? "true" : "false")
              : rawVal;
        while (cells.length <= idx) cells.push("");
        cells[idx] = text;
      }
      parsedRows.push(cells);
    }
    if (parsedRows.length === 0) continue;
    const columns = parsedRows[0].map((col) => col.trim());
    const rows: string[][] = [];
    for (const row of parsedRows.slice(1)) {
      if (!row.join("").trim()) continue;
      const line = Array.from({ length: columns.length }, (_, idx) => row[idx] ?? "");
      rows.push(line);
    }
    if (columns.some(Boolean)) sheets.push({ name, columns, rows });
  }
  return sheets.length > 0 ? sheets : null;
}

function postImportCsv(type: string, rows: Record<string, string>[], preview: boolean): Promise<Api.ImportRow[]> {
  if (rows.length === 0) return Promise.resolve([]);
  const names = Object.keys(rows[0]);
  const csv = [names, ...rows.map((row) => names.map((name) => row[name] ?? ""))]
    .map((cells) => cells.map(quoteCsv).join(","))
    .join("\r\n");
  return request<Api.ImportRow[]>(
    `/v1/import/${encodeURIComponent(type)}${preview ? "?preview=true" : ""}`,
    { body: new Blob([csv], { type: "text/csv" }) },
  );
}

function writableFields(described: Described) {
  return (described.create?.payload ?? []).flatMap((parameter) => {
    const field = described.field(parameter.name);
    return field && !field.readOnly && field.type !== "lines" && field.type !== "attachment" ? [field] : [];
  });
}

function choiceOptions(field: Api.FieldInfo): PlanField["choices"] {
  if (field.type !== "choice") return undefined;
  return field.choices?.map((key, index) => {
    const title = field.choiceTitles?.[index] ?? key;
    return { key, labels: [title, t(title)] };
  });
}

export function importChoice(raw: string, options?: PlanField["choices"]): string {
  if (!options?.length || !raw.trim()) return raw;
  const value = raw.trim();
  if (options.some((option) => option.key === value)) return value;
  const matches = options.filter((option) => [option.key, ...option.labels].some((label) => plainKey(label) === plainKey(value)));
  return matches.length === 1 ? matches[0].key : raw;
}

function prefixBaseResult(result: Api.ImportRow): Api.ImportRow {
  const outcome = result.outcome.startsWith(unreadable)
    ? `${unreadable}base.${result.outcome.slice(unreadable.length)}`
    : result.outcome;
  const issues = result.error?.issues?.map((issue) => ({
    ...issue,
    path: issue.path.length > 0 ? [`base.${issue.path[0]}`, ...issue.path.slice(1)] : issue.path,
  }));
  return result.error ? { ...result, outcome, error: { ...result.error, issues } } : { ...result, outcome };
}

export function buildObjectPlan(described: Described, byType: Map<string, Described>): ObjectPlan {
  const extType = described.entity.extends;
  const base = extType && described.field("base")?.ref === extType ? byType.get(extType) : undefined;
  const fields: PlanField[] = [];
  let displayKey = described.entity.display;
  if (base) {
    const baseWritable = writableFields(base);
    const baseDisplay = baseWritable.find((field) => field.name === base.entity.display);
    const derivedDisplay = described.field(described.entity.display);
    if (baseDisplay) {
      const shownTitle = derivedDisplay?.title || baseDisplay.title;
      displayKey = `base.${baseDisplay.name}`;
      fields.push({
        key: displayKey,
        field: baseDisplay.name,
        title: shownTitle,
        aliases: [
          baseDisplay.name, baseDisplay.title, t(baseDisplay.title),
          described.entity.title, t(described.entity.title),
          ...(derivedDisplay ? [derivedDisplay.name, derivedDisplay.title, t(derivedDisplay.title)] : []),
        ],
        source: "base",
        ref: baseDisplay.ref,
        required: baseDisplay.required,
        choices: choiceOptions(baseDisplay),
      });
    }
    for (const field of writableFields(described)) {
      if (field.name === "base" || field.provenance === "from") continue;
      fields.push({
        key: field.name,
        field: field.name,
        title: field.title,
        aliases: [field.name, field.title, t(field.title)],
        source: "own",
        ref: field.ref,
        required: field.required,
        choices: choiceOptions(field),
      });
    }
    for (const field of baseWritable) {
      if (field.name === baseDisplay?.name) continue;
      const collision = fields.some((existing) => plainKey(existing.title) === plainKey(field.title));
      const label = collision ? `${base.entity.title} · ${field.title}` : field.title;
      fields.push({
        key: `base.${field.name}`,
        field: field.name,
        title: label,
        aliases: collision ? [`base.${field.name}`, label] : [field.name, field.title, t(field.title)],
        source: "base",
        ref: field.ref,
        required: field.required,
        choices: choiceOptions(field),
      });
    }
  } else {
    for (const field of writableFields(described)) {
      fields.push({
        key: field.name,
        field: field.name,
        title: field.title,
        aliases: [field.name, field.title, t(field.title)],
        source: "own",
        ref: field.ref,
        required: field.required,
        choices: choiceOptions(field),
      });
    }
  }
  const byKey = new Map(fields.map((field) => [field.key, field]));
  return {
    type: described.type,
    baseType: base?.type,
    title: described.plural,
    displayKey,
    fields,
    withId: !base,
    run: async (lines, used, preview, pendingRefs) => {
      const results = new Map<number, Api.ImportRow>();
      const baseIds = new Map<number, string>();
      if (lines.length === 0) return { results, baseIds };
      const cellValue = (line: PlanLine, spec: PlanField | undefined, index: number) => {
        const raw = line.cells[index] ?? "";
        if (preview && spec?.ref && !spec.required && raw.trim() && pendingRefs.get(spec.ref)?.has(plainKey(raw))) {
          return "";
        }
        return importChoice(raw, spec?.choices);
      };
      if (!base) {
        const rows = lines.map((line) => Object.fromEntries(used.map(({ name, index }) => [name, cellValue(line, byKey.get(name), index)])));
        const answered = await postImportCsv(described.type, rows, preview);
        lines.forEach((line, idx) => { if (answered[idx]) results.set(line.row, answered[idx]); });
        return { results, baseIds };
      }
      const baseUsed = used.flatMap(({ name, index }) => {
        const spec = byKey.get(name);
        return spec?.source === "base" ? [{ spec, index }] : [];
      });
      const ownUsed = used.flatMap(({ name, index }) => {
        const spec = byKey.get(name);
        return spec?.source === "own" ? [{ spec, index }] : [];
      });
      if (preview) {
        const baseRows = lines.map((line) => Object.fromEntries(baseUsed.map(({ spec, index }) => [spec.field, cellValue(line, spec, index)])));
        const baseAnswered = baseUsed.length > 0
          ? await postImportCsv(base.type, baseRows, true)
          : lines.map((line) => ({ row: line.row, id: "", action: "", outcome: "ok" }));
        const ownAnswered = ownUsed.length > 0
          ? await postImportCsv(described.type, lines.map((line) => Object.fromEntries(ownUsed.map(({ spec, index }) => [spec.field, cellValue(line, spec, index)]))), true)
          : [];
        lines.forEach((line, idx) => {
          const bRes = baseAnswered[idx];
          if (bRes && bRes.outcome !== "ok") {
            results.set(line.row, prefixBaseResult(bRes));
            return;
          }
          const oRes = ownAnswered[idx];
          if (!oRes || oRes.outcome === "ok") {
            results.set(line.row, { row: line.row, id: "", action: `${described.type}.create`, outcome: "ok" });
            return;
          }
          const nonBaseIssues = (oRes.error?.issues ?? []).filter((issue) => issue.path[0] !== "base");
          if ((oRes.error?.issues?.length ?? 0) > 0 && nonBaseIssues.length === 0) {
            results.set(line.row, { row: line.row, id: "", action: `${described.type}.create`, outcome: "ok" });
            return;
          }
          results.set(line.row, nonBaseIssues.length > 0 && oRes.error ? { ...oRes, error: { ...oRes.error, issues: nonBaseIssues } } : oRes);
        });
        return { results, baseIds };
      }
      const needBase = lines.filter((line) => !line.baseId);
      if (needBase.length > 0) {
        const baseRows = needBase.map((line) => Object.fromEntries(baseUsed.map(({ spec, index }) => [spec.field, importChoice(line.cells[index] ?? "", spec.choices)])));
        const baseAnswered = await postImportCsv(base.type, baseRows, false);
        needBase.forEach((line, idx) => {
          const res = baseAnswered[idx];
          if (res?.outcome === "ok" && res.id) baseIds.set(line.row, res.id);
          else if (res) results.set(line.row, prefixBaseResult(res));
        });
      }
      const readyExt = lines.filter((line) => Boolean(line.baseId || baseIds.get(line.row)));
      if (readyExt.length > 0) {
        const extRows = readyExt.map((line) => ({
          base: line.baseId ?? baseIds.get(line.row) ?? "",
          ...Object.fromEntries(ownUsed.map(({ spec, index }) => [spec.field, importChoice(line.cells[index] ?? "", spec.choices)])),
        }));
        const extAnswered = await postImportCsv(described.type, extRows, false);
        readyExt.forEach((line, idx) => { if (extAnswered[idx]) results.set(line.row, extAnswered[idx]); });
      }
      return { results, baseIds };
    },
  };
}

export function matchSheetPlan(sheetName: string, primary: ObjectPlan, plans: ObjectPlan[]): ObjectPlan {
  const key = plainKey(sheetName);
  const space = primary.type.includes(".") ? primary.type.split(".")[0] : "";
  const ordered = [...plans.filter((plan) => space && plan.type.startsWith(`${space}.`)), ...plans.filter((plan) => !space || !plan.type.startsWith(`${space}.`))];
  for (const plan of ordered) {
    const bare = plan.type.split(".").pop() ?? plan.type;
    if ([plan.title, t(plan.title), bare, `${bare}s`, plan.type].some((candidate) => plainKey(candidate) === key)) return plan;
  }
  return primary;
}

export function topologicalOrder<T extends { plan: ObjectPlan; mapping: string[] }>(sheets: T[]): number[] {
  const indices = sheets.map((_, idx) => idx);
  const providedBy = new Map<string, number>();
  sheets.forEach((sheet, idx) => {
    providedBy.set(sheet.plan.type, idx);
    if (sheet.plan.baseType) providedBy.set(sheet.plan.baseType, idx);
  });
  const deps = new Map<number, Set<number>>();
  sheets.forEach((sheet, idx) => {
    const needed = new Set<number>();
    for (const mappedKey of sheet.mapping) {
      const field = sheet.plan.fields.find((candidate) => candidate.key === mappedKey);
      const depIdx = field?.ref ? providedBy.get(field.ref) : undefined;
      if (depIdx !== undefined && depIdx !== idx) needed.add(depIdx);
    }
    deps.set(idx, needed);
  });
  const visited = new Set<number>();
  const visiting = new Set<number>();
  const order: number[] = [];
  const visit = (idx: number) => {
    if (visited.has(idx) || visiting.has(idx)) return;
    visiting.add(idx);
    for (const dep of deps.get(idx) ?? []) visit(dep);
    visiting.delete(idx);
    visited.add(idx);
    order.push(idx);
  };
  for (const idx of indices) visit(idx);
  return order;
}
