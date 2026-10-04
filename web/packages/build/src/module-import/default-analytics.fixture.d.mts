import type {SourceModule,ImportBindings,ImportTarget} from './compile';
export const analyticFacetIDs:string[];
export const analyticChartIDs:string[];
export function defaultAnalyticsFixture(profile:string):{module:SourceModule;bindings:ImportBindings;target:ImportTarget};
export function defaultAnalyticsFacetGroup(profile:string):{module:SourceModule;bindings:ImportBindings;target:ImportTarget};
export function defaultAnalyticsChartGroup(profile:string):{module:SourceModule;bindings:ImportBindings;target:ImportTarget};
export function defaultAnalyticsSelectionGroup(profile:string):{module:SourceModule;bindings:ImportBindings;target:ImportTarget};
export function defaultAnalyticsComparisonGroup(profile:string):{module:SourceModule;bindings:ImportBindings;target:ImportTarget};
