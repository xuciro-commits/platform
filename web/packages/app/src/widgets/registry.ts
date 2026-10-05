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
/** Runtime and authoring registrations consume the same complete contract set. */
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

/** Controlled build-time registration. Implementations must cover the shared
 * contract exactly; adding a manifest alone does not make a widget runnable.
 * There is no tenant script loading or permission policy in this registry.
 */
export class WidgetRegistry<Context> {
  private readonly implementations: Readonly<Record<WidgetID, WidgetImplementation<Context>>>;
  constructor(implementations: Record<WidgetID, ComponentType<Context>|WidgetImplementation<Context>>) {
    const entries=Object.fromEntries(Object.entries(implementations).map(([id,implementation])=>{
      if(!implementation)throw new Error(`Missing widget implementation: ${id}`);
      const plugin=typeof implementation==='object'&&'Renderer' in implementation?implementation:undefined;
      if(plugin&&plugin.contract.componentID!==id)throw new Error(`Mismatched widget plugin: ${id}`);
      return [id,{configVersion:plugin?.contract.configVersion??1,Renderer:plugin?.Renderer??implementation as ComponentType<Context>}];
    })) as Record<WidgetID,WidgetDefinition&{Renderer:ComponentType<Context>}>;
    createWidgetDefinitions(entries);
    this.implementations = Object.freeze(Object.fromEntries(widgetContracts.map(contract=>[contract.componentID,Object.freeze({contract,Renderer:entries[contract.componentID].Renderer})])) as Record<WidgetID,WidgetImplementation<Context>>);
  }
  resolve(id: string, version = 1): ComponentType<Context> | undefined {
    return this.resolveDefinition(id,version)?.Renderer;
  }
  resolveDefinition(id:string,version:number):WidgetImplementation<Context>|undefined {
    const contract = widgetContract(id);
    return contract?.configVersion === version ? this.implementations[contract.componentID] : undefined;
  }
}

export const createWidgetRegistry = <Context,>(implementations: Record<WidgetID, ComponentType<Context>|WidgetImplementation<Context>>) => new WidgetRegistry<Context>(implementations);
