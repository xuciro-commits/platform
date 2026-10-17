import { pageUIManifest } from "@platform/kernel";
import type { ComponentType } from "react";

export const pageUIProfile = pageUIManifest.uiProfile;
export const supportsPageUIProfile = (profile: string) => (pageUIManifest.supportedProfiles as readonly string[]).includes(profile);
export const widgetContracts = pageUIManifest.widgets;
export type WidgetContract = typeof widgetContracts[number];
export type WidgetID = WidgetContract["componentID"];
export type WidgetImplementation<Context> = { contract: WidgetContract; Renderer: ComponentType<Context> };

export const widgetContract = (id: string): WidgetContract | undefined => widgetContracts.find((contract) => contract.componentID === id);

export type WidgetDefinition={configVersion:number};
/** Public metadata registry for editor extensions; runtime renderers use WidgetRegistry. */
export function createWidgetDefinitions<T extends Record<WidgetID,WidgetDefinition>>(definitions:T){
 for(const id of Object.keys(definitions))if(!widgetContract(id))throw new Error(`Undeclared widget implementation: ${id}`);
 for(const contract of widgetContracts){
  const definition=definitions[contract.componentID];
  if(!definition)throw new Error(`Missing widget implementation: ${contract.componentID}`);
  if(definition.configVersion!==contract.configVersion)throw new Error(`Unsupported widget configuration: ${contract.componentID}`);
  if('lifecyclePolicy' in contract){
   const policy=contract.lifecyclePolicy;
   const session=policy.stateOwner==='page-session'&&policy.hidden==='retain'&&JSON.stringify(policy.clearOn)===JSON.stringify(['scope-change','binding-change','close']);
   const instance=policy.stateOwner==='widget-instance'&&policy.hidden==='unmount'&&JSON.stringify(policy.clearOn)===JSON.stringify(['record','member','definition','scope-close']);
   if(!session&&!instance)throw new Error(`Unsupported widget lifecycle: ${contract.componentID}`);
  }
 }
 const entries=Object.freeze(Object.fromEntries(Object.entries(definitions).map(([id,value])=>[id,Object.freeze({...value})])) as T);
 return Object.freeze({entries,resolve:(id:string,version:number):T[WidgetID]|undefined=>{const contract=widgetContract(id);return contract?.configVersion===version?entries[contract.componentID]:undefined;}});
}

/** Runtime renderers are the only registry. The editor uses the same manifest
 * as a compile-time keyed table, so it does not need a second runtime registry. */
export class WidgetRegistry<Context> {
  private readonly implementations: Readonly<Record<WidgetID, WidgetImplementation<Context>>>;

  constructor(implementations: Record<WidgetID, ComponentType<Context> | WidgetImplementation<Context>>) {
    for (const id of Object.keys(implementations)) {
      if (!widgetContract(id)) throw new Error(`Undeclared widget implementation: ${id}`);
    }

    this.implementations = Object.freeze(Object.fromEntries(widgetContracts.map((contract) => {
      const implementation = implementations[contract.componentID];
      if (!implementation) throw new Error(`Missing widget implementation: ${contract.componentID}`);
      const plugin = typeof implementation === "object" && "Renderer" in implementation ? implementation : undefined;
      if (plugin && plugin.contract.componentID !== contract.componentID) throw new Error(`Mismatched widget plugin: ${contract.componentID}`);
      const version = plugin?.contract.configVersion ?? 1;
      if (version !== contract.configVersion) throw new Error(`Unsupported widget configuration: ${contract.componentID}`);
      return [contract.componentID, Object.freeze({ contract, Renderer: plugin?.Renderer ?? implementation as ComponentType<Context> })];
    })) as Record<WidgetID, WidgetImplementation<Context>>);
  }

  resolve(id: string, version = 1): ComponentType<Context> | undefined {
    return this.resolveDefinition(id, version)?.Renderer;
  }

  resolveDefinition(id: string, version: number): WidgetImplementation<Context> | undefined {
    const implementation = this.implementations[id as WidgetID];
    return implementation?.contract.configVersion === version ? implementation : undefined;
  }
}

export const createWidgetRegistry = <Context,>(implementations: Record<WidgetID, ComponentType<Context> | WidgetImplementation<Context>>) => new WidgetRegistry<Context>(implementations);
