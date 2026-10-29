import { isAlias, isMap, isScalar, isSeq, parseAllDocuments, stringify } from 'yaml';
import type { Api } from '@platform/kernel';
import { inputIssues } from './entity';
import { applies } from './types';
import { t } from '../i18n';

export type SourceFormat = 'json' | 'yaml';
export type StructuredValue = Record<string, unknown>;
export function structuredExample(fields: Api.FieldInfo[]): StructuredValue {
  return Object.fromEntries(fields.filter(field=>!field.readOnly).map(field=>{
    let value: unknown;
    if (field.example !== undefined) {
      if (['text','longtext','reference','date','datetime','choice'].includes(field.type)) value=field.example;
      else { try { value=JSON.parse(field.example); } catch { value=field.example; } }
    } else if (field.choices?.length) value=field.choices[0];
    else if (field.type==='reference') value=t('<select a permitted record>');
    else if (field.type==='references') value=[t('<select a permitted record>')];
    else if (field.type==='lines') value=field.fields?[structuredExample(field.fields)]:[];
    else if (field.type==='tags') value=[];
    else if (field.type==='json') value={};
    else if (field.type==='boolean') value=false;
    else if (['integer','decimal','money'].includes(field.type)) value=field.constraints?.min??0;
    else if (field.type==='date') value='YYYY-MM-DD';
    else if (field.type==='datetime') value='YYYY-MM-DDTHH:mm:ssZ';
    else value='<'+field.title+'>';
    return [field.name,value];
  }));
}
const decimal = (raw: string) => {
  const match = /^([+-]?)([0-9]+)(?:\.([0-9]*))?(?:e([+-]?[0-9]+))?$/i.exec(raw);
  if (!match) return raw;
  let digits = (match[2]! + (match[3] ?? '')).replace(/^0+/, '') || '0';
  let exponent = Number(match[4] ?? 0) - (match[3]?.length ?? 0);
  while (digits.length > 1 && digits.endsWith('0')) { digits = digits.slice(0, -1); exponent++; }
  return (digits === '0' ? '' : match[1] === '-' ? '-' : '') + digits + 'e' + (digits === '0' ? 0 : exponent);
};

export function parseStructuredSource(raw: string, format: SourceFormat): StructuredValue {
  if (format === 'json') JSON.parse(raw);
  const documents = parseAllDocuments(raw, { version: '1.2', uniqueKeys: true, intAsBigInt: true, prettyErrors: true });
  if (documents.length !== 1) throw Error(t('Enter one configuration document.'));
  const doc = documents[0]!;
  if (doc.errors.length) throw Error(doc.errors[0]!.message);
  const convert = (node: unknown): unknown => {
    if (node == null) return null;
    if (isAlias(node) || (typeof node === 'object' && ('anchor' in node && node.anchor || 'tag' in node && node.tag))) throw Error(t('YAML tags, anchors and aliases are not supported.'));
    if (isMap(node)) {
      const result: StructuredValue = {};
      for (const pair of node.items) {
        if (!isScalar(pair.key) || typeof pair.key.value !== 'string') throw Error(t('Configuration keys must be strings.'));
        Object.defineProperty(result, pair.key.value, { value: convert(pair.value), enumerable: true, writable: true, configurable: true });
      }
      return result;
    }
    if (isSeq(node)) return node.items.map(convert);
    if (!isScalar(node)) throw Error(t('Enter a JSON-compatible configuration.'));
    const value = node.value;
    if (typeof value === 'bigint') {
      const number = Number(value);
      if (!Number.isSafeInteger(number)) throw Error(t('This number cannot be represented without losing precision.'));
      return number;
    }
    if (typeof value === 'number' && (!Number.isFinite(value) || Number.isInteger(value) && !Number.isSafeInteger(value) || node.source && /^[+-]?[0-9]/.test(node.source) && !node.format && decimal(node.source) !== decimal(String(value)))) throw Error(t('This number cannot be represented without losing precision.'));
    if (value !== null && !['string', 'number', 'boolean'].includes(typeof value)) throw Error(t('Enter a JSON-compatible configuration.'));
    return value;
  };
  const value = convert(doc.contents);
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw Error(t('The configuration must be an object.'));
  return value as StructuredValue;
}

export function formatStructuredSource(value: StructuredValue, format: SourceFormat): string {
  return format === 'json' ? JSON.stringify(value, null, 2) : stringify(JSON.parse(JSON.stringify(value)), { aliasDuplicateObjects: false, lineWidth: 0 });
}

export function structuredIssues(value: StructuredValue, fields: Api.FieldInfo[]): Api.FieldIssue[] {
  const issues: Api.FieldIssue[] = [];
  const add = (path: string[], code: string, message: string) => issues.push({path, code, message});
  for (const key of Object.keys(value)) {
    const field = fields.find(field => field.name === key);
    if (!field) add([key], 'unknown', t('This field is not declared.'));
    else if (field.readOnly) add([key], 'readonly', t('This field is read only.'));
  }
  const inputs: Api.Field[] = [];
  for (const field of fields) {
    if (field.readOnly || !applies(field, value)) continue;
    inputs.push({name: field.name, type: ({text:'string',longtext:'string',reference:'string',choice:'string',integer:'integer',decimal:'number',money:'number',references:'string[]',tags:'string[]'} as Record<string,string>)[field.type] ?? field.type, required: field.required, description: field.title, choices: field.choices, constraints: field.constraints ?? {}});
    const v = value[field.name];
    if (v === undefined || v === null || v === '') continue;
    if (['lines','tags','references'].includes(field.type) && (!Array.isArray(v) || field.type !== 'lines' && v.some(item => typeof item !== 'string'))) add([field.name], 'type', t('Enter a valid value.'));
    if (field.type === 'lines' && Array.isArray(v)) for (const [index, row] of v.entries()) {
      if (!row || typeof row !== 'object' || Array.isArray(row)) add([field.name,String(index)], 'type', t('The configuration must be an object.'));
      else if (field.fields) for (const issue of structuredIssues(row as StructuredValue, field.fields)) issues.push({...issue,path:[field.name,String(index),...issue.path]});
    }
    if (field.required && Array.isArray(v) && !v.length) add([field.name], 'required', t('Enter a value.'));
  }
  for (const issue of inputIssues(inputs, value)) if (!issues.some(item => item.code === issue.code && item.path.join('.') === issue.path.join('.'))) issues.push(issue);
  return issues;
}
