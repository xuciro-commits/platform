import {defaultOverviewFixture} from './default-overview.fixture.mjs';
export const analyticFacetIDs=['wPills','wTerms','wHeatmap','wTreemap','wHistogram'];
export const analyticChartIDs=['wChart1','wChart2','wChart3','wChart4','wPivot'];
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
/** Five original chart configurations and explicit query/input controls only. */
export function defaultAnalyticsChartGroup(profile){
 const f=defaultAnalyticsFixture(profile),original=f.module,variables=new Set(['filteredAssets','statusFilter','priorityFilter','ownerFilter','searchText','pressureMin','pressureMax']);
 f.module={id:'original-analytics-chart-group',name:'Original five Analytics charts',pages:[{id:'pAnalytics',name:'Analytics chart group',rootSectionId:'root'}],sections:{root:{id:'root',name:'Group',layout:'rows',children:['search','control',...analyticChartIDs].map(id=>({kind:'widget',id}))}},widgets:Object.fromEntries(analyticChartIDs.map(id=>[id,structuredClone(original.widgets[id])])),variables:original.variables.filter(v=>variables.has(v.id)).map(v=>structuredClone(v)),overlays:[],unusedWidgetIds:[]};
 f.module.widgets.search={id:'search',name:'Original search control',type:'TextInput',config:{variableId:'searchText',label:'Original search'}};
 f.module.widgets.control={id:'control',name:'Original query window',type:'ObjectTable',config:{objectSetVarId:'filteredAssets',columns:[{key:'name'}]}};
 f.bindings={objects:{Asset:f.target.object},fields:{Asset:f.bindings.fields.Asset},actions:{},queries:{}};return f;
}

/** Original ScatterPlot, Leaderboard and PropertyList share the original record
 * interface; its Operations producer metadata stays present, but is not mounted.
 * Comparison and complete-page acceptance remain separate requirements. */
export function defaultAnalyticsSelectionGroup(profile){
 const f=defaultAnalyticsFixture(profile),original=f.module,ids=['wScatter','wLeader','wPropList1','wObjectTable1'],variables=new Set(['filteredAssets','statusFilter','priorityFilter','ownerFilter','searchText','pressureMin','pressureMax','selectedAsset']);
 f.module={id:'original-analytics-selection-group',name:'Original Analytics shared selection',pages:[{id:'pAnalytics',name:'Analytics selection group',rootSectionId:'root'}],sections:{root:{id:'root',name:'Group',layout:'rows',children:['search','wScatter','wLeader','wPropList1'].map(id=>({kind:'widget',id}))}},widgets:Object.fromEntries(ids.map(id=>[id,structuredClone(original.widgets[id])])),variables:original.variables.filter(v=>variables.has(v.id)).map(v=>structuredClone(v)),moduleInterface:original.moduleInterface.filter(p=>p.variableId==='selectedAsset'),overlays:[],unusedWidgetIds:[]};
 f.module.widgets.search={id:'search',name:'Original search control',type:'TextInput',config:{variableId:'searchText',label:'Original search'}};
 f.bindings={objects:{Asset:f.target.object},fields:{Asset:f.bindings.fields.Asset},actions:{},queries:{},application:{binding:f.bindings.application.binding,ports:{selectedAsset:f.bindings.application.ports.selectedAsset}},scatters:{wScatter:{labelField:'name'}},leaderboards:{wLeader:{labelField:'name'}}};return f;
}

/** Original Operations table and Analytics selection/comparison configs, with
 * explicit navigation controls. This proves the cross-page group, not the full page. */
export function defaultAnalyticsComparisonGroup(profile){
 const f=defaultAnalyticsFixture(profile),original=f.module,ids=['wObjectTable1','wScatter','wLeader','wCompare','wPropList1'],variables=new Set(['filteredAssets','statusFilter','priorityFilter','ownerFilter','searchText','pressureMin','pressureMax','selectedAsset','selectedAssets','currentPage','showDetail']);
 f.module={id:'original-analytics-comparison-group',name:'Original cross-page selection and comparison',pages:[{id:'pOperations',name:'Operations selection group',rootSectionId:'operations'},{id:'pAnalytics',name:'Analytics comparison group',rootSectionId:'analytics'}],sections:{operations:{id:'operations',name:'Operations group',layout:'rows',children:['search','wObjectTable1','wPropList1'].map(id=>({kind:'widget',id}))},analytics:{id:'analytics',name:'Analytics group',layout:'rows',children:['wScatter','wLeader','wCompare'].map(id=>({kind:'widget',id}))}},widgets:Object.fromEntries(ids.map(id=>[id,structuredClone(original.widgets[id])])),variables:original.variables.filter(v=>variables.has(v.id)).map(v=>structuredClone(v)),moduleInterface:structuredClone(original.moduleInterface),overlays:[],unusedWidgetIds:[]};
 f.module.widgets.search={id:'search',name:'Original search control',type:'TextInput',config:{variableId:'searchText',label:'Original search'}};
 f.bindings={objects:{Asset:f.target.object},fields:{Asset:f.bindings.fields.Asset},actions:{},queries:{},edits:f.bindings.edits,application:{...f.bindings.application,recordSets:{selectedAssets:{variable:'records',writable:true}}},scatters:{wScatter:{labelField:'name'}},leaderboards:{wLeader:{labelField:'name'}},comparisons:{wCompare:{labelField:'name',fields:['owner','pressure','temperature']}}};
 f.target.definitions[0].application.variables.records={scope:'application',type:'record-set',mode:'resource',source:{kind:'record-set',object:{app:'build',kind:'object',name:f.target.object}}};return f;
}
