import {readFileSync} from 'node:fs';
const original=JSON.parse(readFileSync(new URL('./default.workshop.json',import.meta.url),'utf8'));
/** Five untouched consumer configs from the full source, with explicit test controls.
 * This group fixture does not represent successful import of the full Module. */
export function defaultConsumerGroup(){
 const widgets=Object.fromEntries(['wObjectSetTitle1','wStatusTracker','wLinks1','wObjectSetTitle1~7','wLinks1~9','wSearch1'].map(id=>[id,structuredClone(original.widgets[id])]));
 widgets.wObjectTable1={id:'wObjectTable1',name:'Original asset selector',type:'ObjectTable',config:{objectSetVarId:'filteredAssets',activeVarId:'selectedAsset',columns:[{key:'name'}]}};
 const overlay=structuredClone(original.overlays.find(o=>o.id==='oAssetDrawer'));
 widgets.open={id:'open',name:'Open original consumer drawer',type:'SingleButton',config:{label:'Open original consumer drawer',eventActions:[{kind:'openOverlay',overlayId:overlay.id}]}};
 const ids=new Set(['filteredAssets','statusFilter','priorityFilter','ownerFilter','searchText','pressureMin','pressureMax','selectedAsset',overlay.openVariableId]);
 return {id:'default-consumer-group',name:'Original collection and relation consumers',pages:[{id:'pOperations',name:'Original consumer task',rootSectionId:'group'}],sections:{group:{id:'group',name:'Group',layout:'rows',children:['wSearch1','wObjectTable1','wObjectSetTitle1','wStatusTracker','wLinks1','open'].map(id=>({kind:'widget',id}))},[overlay.rootSectionId]:{id:overlay.rootSectionId,name:'Original drawer consumers',layout:'rows',children:['wObjectSetTitle1~7','wLinks1~9'].map(id=>({kind:'widget',id}))}},widgets,variables:original.variables.filter(v=>ids.has(v.id)).map(v=>structuredClone(v)),overlays:[overlay],moduleInterface:original.moduleInterface.filter(p=>p.variableId==='selectedAsset').map(p=>structuredClone(p)),unusedWidgetIds:[]};
}
