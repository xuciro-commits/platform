import {defaultOverviewFixture} from './default-overview.fixture.mjs';
export const analyticFacetIDs=['wPills','wTerms','wHeatmap','wTreemap','wHistogram'];
/** Full source diagnostics; no unported Analytics component is removed here. */
export function defaultAnalyticsFixture(profile){
 const f=defaultOverviewFixture(profile);f.bindings.regions={sAnalyticsRoot:{maxHeight:960},sDrawerRoot:{maxHeight:640}};f.bindings.explorations={statusFilter:{Active:'active',Warning:'warning',Offline:'offline',Maintenance:'maintenance'}};f.bindings.fields.Asset.revenueImpact='exposure';return f;
}
/** Five untouched source configs plus a shared query-window control for group proof.
 * This is a group fixture, not successful import of the complete Analytics page. */
export function defaultAnalyticsFacetGroup(profile){
 const f=defaultAnalyticsFixture(profile),original=f.module,variables=new Set(['filteredAssets','statusFilter','priorityFilter','ownerFilter','searchText','pressureMin','pressureMax']);
 f.module={id:'original-analytics-facet-group',name:'Original five Analytics facets',pages:[{id:'pAnalytics',name:'Analytics facet group',rootSectionId:'root'}],sections:{root:{id:'root',name:'Group',layout:'rows',children:['control',...analyticFacetIDs].map(id=>({kind:'widget',id}))}},widgets:Object.fromEntries(analyticFacetIDs.map(id=>[id,structuredClone(original.widgets[id])])),variables:original.variables.filter(v=>variables.has(v.id)).map(v=>structuredClone(v)),overlays:[],unusedWidgetIds:[]};
 f.module.widgets.control={id:'control',name:'Original query window',type:'ObjectTable',config:{objectSetVarId:'filteredAssets',columns:[{key:'name'},{key:'owner'}]}};
 f.bindings={objects:{Asset:f.target.object},fields:{Asset:f.bindings.fields.Asset},actions:{},queries:{},explorations:f.bindings.explorations};return f;
}
