import { pageUIManifest } from "@platform/kernel";
import type { ComponentType } from "react";

export const pageUIProfile = pageUIManifest.uiProfile;
export const supportsPageUIProfile = (profile: string) => (pageUIManifest.supportedProfiles as readonly string[]).includes(profile);
export const widgetContracts = pageUIManifest.widgets;
export type WidgetContract = typeof widgetContracts[number];
export type WidgetID = WidgetContract["componentID"];
export type WidgetImplementation<Context> = { contract: WidgetContract; Renderer: ComponentType<Context> };

export const widgetContract = (id: string): WidgetContract | undefined => widgetContracts.find((contract) => contract.componentID === id);

/** Controlled build-time registration. Implementations must cover the shared
 * contract exactly; adding a manifest alone does not make a widget runnable.
 * There is no tenant script loading or permission policy in this registry.
 */
export class WidgetRegistry<Context> {
  private readonly implementations: Readonly<Record<WidgetID, WidgetImplementation<Context>>>;
  constructor(implementations: Record<WidgetID, ComponentType<Context>>) {
    for(const id of Object.keys(implementations))if(!widgetContract(id))throw new Error(`Undeclared widget implementation: ${id}`);
    for (const contract of widgetContracts) {
      if (!implementations[contract.componentID]) throw new Error(`Missing widget implementation: ${contract.componentID}`);
      if("lifecyclePolicy" in contract) {
        const policy=contract.lifecyclePolicy;
        if(policy.stateOwner!=="page-session"||policy.hidden!=="retain"||JSON.stringify(policy.clearOn)!==JSON.stringify(["scope-change","binding-change","close"]))throw new Error(`Unsupported widget lifecycle: ${contract.componentID}`);
      }
    }
    this.implementations = Object.freeze(Object.fromEntries(widgetContracts.map(contract=>[contract.componentID,Object.freeze({contract,Renderer:implementations[contract.componentID]})])) as Record<WidgetID,WidgetImplementation<Context>>);
  }
  resolve(id: string, version = 1): ComponentType<Context> | undefined {
    return this.resolveDefinition(id,version)?.Renderer;
  }
  resolveDefinition(id:string,version:number):WidgetImplementation<Context>|undefined {
    const contract = widgetContract(id);
    return contract?.configVersion === version ? this.implementations[contract.componentID] : undefined;
  }
}

export const createWidgetRegistry = <Context,>(implementations: Record<WidgetID, ComponentType<Context>>) => new WidgetRegistry<Context>(implementations);
