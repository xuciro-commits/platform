import { pageUIManifest } from "@platform/kernel";
import type { ComponentType } from "react";

export const pageUIProfile = pageUIManifest.uiProfile;
export const supportsPageUIProfile = (profile: string) => (pageUIManifest.supportedProfiles as readonly string[]).includes(profile);
export const widgetContracts = pageUIManifest.widgets;
export type WidgetContract = typeof widgetContracts[number];
export type WidgetID = WidgetContract["componentID"];

export const widgetContract = (id: string): WidgetContract | undefined => widgetContracts.find((contract) => contract.componentID === id);

/** Controlled build-time registration. Implementations must cover the shared
 * contract exactly; adding a manifest alone does not make a widget runnable.
 * There is no tenant script loading or permission policy in this registry.
 */
export class WidgetRegistry<Context> {
  private readonly implementations: Readonly<Record<WidgetID, ComponentType<Context>>>;
  constructor(implementations: Record<WidgetID, ComponentType<Context>>) {
    for (const contract of widgetContracts) {
      if (!implementations[contract.componentID]) throw new Error(`Missing widget implementation: ${contract.componentID}`);
    }
    this.implementations = Object.freeze({ ...implementations });
  }
  resolve(id: string, version = 1): ComponentType<Context> | undefined {
    const contract = widgetContract(id);
    return contract?.configVersion === version ? this.implementations[contract.componentID] : undefined;
  }
}

export const createWidgetRegistry = <Context,>(implementations: Record<WidgetID, ComponentType<Context>>) => new WidgetRegistry<Context>(implementations);
