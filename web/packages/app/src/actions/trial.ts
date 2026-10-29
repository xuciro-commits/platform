import type { Api } from '@platform/kernel';
import {t} from '@platform/ui';
import { useHost } from '../index';

export function useActionTrial() {
  const {client, catalog} = useHost();
  return (schema:string,target:{type:string;id:string},values:Record<string,unknown>,revision?:number):Promise<Api.Simulation> => {
    const action = catalog.find(action=>action.schema===schema&&action.target===target.type);
    if (!action) return Promise.reject(Error(t('The original action is unavailable.')));
    const payload = btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify(values))));
    const submission = {target,schema:{name:schema,version:action.schemaVersion?.family??1},payload,...revision!==undefined?{expectedRevision:revision}:{}};
    return client.get<Api.Simulation>('/v1/simulate'+String.fromCharCode(10)+JSON.stringify(submission),true);
  };
}
