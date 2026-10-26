import {useId} from 'react';
import {Button,Panel,RecordLookup,Select,t,type RecordSource} from '@platform/ui';
import type {Api} from '@platform/kernel';
import type {ScalarValue} from '../runtime/decimal';
import type {VariableResult} from '../runtime/variables';
import type {QueryWindow} from './QueryWindowFrame';
import {Facets} from './Facets';
export type FilterProps={object:string;config:Pick<Api.Section,'title'|'fields'|'facets'|'filterSearchVariable'>;source?:RecordSource;filters:Record<string,unknown>;onNarrow:(object:string,field:string,value:unknown)=>void;facetValues?:Record<string,VariableResult>;onFacet?:(id:string,value:ScalarValue)=>void;window?:QueryWindow;aggregateScope?:string};
export function FilterRenderer({object: type,config: section,source,filters: set,onNarrow,facetValues,onFacet,window,aggregateScope}:FilterProps) {
  const prefix = useId();
  if(!source)return <Panel role="alert">{t("No source for records")}</Panel>;
  const info = source.entity(type);
  if(section.facets?.length||section.filterSearchVariable)return <Facets section={section} source={source} object={type} window={window} values={facetValues??{}} onChange={onFacet??(()=>{})} scope={aggregateScope??""}/>;
  return (
    <div role="search" aria-label={section.title || t("Filter")} className="flex flex-wrap items-end gap-3">
      {(section.fields ?? []).map((name) => {
        const f = info?.fields.find((x) => x.name === name);
        if (!f) return null; // not a field this member reads
        const id = `${prefix}-${type}-${name}`;
        const value = set[name];
        return (
          <label key={name} htmlFor={id} className="grid gap-1 text-xs text-muted">{f.title}
            {f.type === "reference" && f.ref
              ? <RecordLookup id={id} source={source} type={f.ref} value={value as string | undefined} onChange={(v) => onNarrow(type, name, v)} />
              : <Select id={id} aria-label={f.title} className="w-40" value={value === undefined ? "" : String(value)}
                  onChange={(e) => onNarrow(type, name, e.target.value === "" ? undefined : f.type === "boolean" ? e.target.value === "true" : e.target.value)}>
                  <option value="">{t("Any")}</option>
                  {f.type === "boolean"
                    ? <><option value="true">{t("Yes")}</option><option value="false">{t("No")}</option></>
                    : (f.choices ?? []).map((c, i) => <option key={c} value={c}>{f.choiceTitles?.[i] ?? c}</option>)}
                </Select>}
          </label>
        );
      })}
      {Object.values(set).some((v) => v !== undefined && v !== "") &&
        <Button size="sm" variant="ghost" onClick={() => Object.keys(set).forEach((name) => onNarrow(type, name, undefined))}>{t("Clear")}</Button>}
    </div>
  );
}
