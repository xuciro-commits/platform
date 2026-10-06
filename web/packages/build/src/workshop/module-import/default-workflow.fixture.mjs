import {defaultMaintenanceFixture} from './default-maintenance.fixture.mjs';
/** Complete Workflow capture with explicit original record, service and retained-page bindings. */
export function defaultWorkflowFixture(profile){
 const f=defaultMaintenanceFixture(profile),asset={app:'build',kind:'object',name:f.target.object};
 f.bindings.regions={sWorkflowRoot:{maxHeight:960},sDrawerRoot:{maxHeight:640}};
 f.bindings.fields.WorkOrder.assetId='asset';f.bindings.fields.WorkOrder.id='id';
 f.bindings.collaboration={wComments:{recordVarId:'selectedWorkOrder'},wMediaUpload:{recordVarId:'selectedWorkOrder'}};
 f.bindings.actionDefaults.wInlineAction=[{parameter:'status',field:'state'},{parameter:'priority',field:'priority'},{parameter:'owner',field:'owner'}];
 f.bindings.actionTables={wActionTable:[{parameter:'status',field:'state'},{parameter:'priority',field:'priority'},{parameter:'owner',field:'owner'}]};
 const conversation={ref:{app:'build',kind:'function',name:'asset-assistant'},sourceVersion:'1.function-1'};
 f.target.definitions.push({ref:conversation.ref,version:conversation.sourceVersion,function:{object:asset.name,conversation:true,fields:['name','state','pressure','temperature','availability'],output:[{name:'reply',type:'string',required:true}]}});
 f.bindings.ai.wAIPChat={migration:'record-scoped-functions',function:conversation,recordVarId:'selectedAsset',replyField:'reply',historyMigration:'original-call-history'};
 const groups=['sensor','alert'].map(name=>({ref:{app:'build',kind:'link-type',name:'asset-'+name},sourceVersion:'1.link-1'}));
 groups.forEach((binding,i)=>f.target.definitions.push({ref:binding.ref,version:binding.sourceVersion,linkType:{name:binding.ref.name,title:i?'Asset alerts':'Asset sensors',parent:asset,child:{app:'build',kind:'object',name:i?'build.alert':'build.sensor'},via:'asset',storage:'reference',cardinality:'one-to-many',deletePolicy:'owner',forward:i?'alerts':'sensors',reverse:'asset'}}));
 f.bindings.vertices={wVertex:{groups}};
 const dashboard={ref:{app:'build',kind:'page',name:'fleet-kpis'},version:'1.page-1',contentVersion:'page.sha256.'+'b'.repeat(64),page:{name:'fleet-kpis',object:asset,sections:[{id:'fleet-count',widget:'metric',title:'Fleet',collectionVariable:'fleet',measure:'count'}],document:{uiProfile:profile,root:'root',nodes:{root:{kind:'rows',children:['count']},count:{kind:'widget',section:'fleet-count'}},variables:{fleet:{scope:'page',type:'object-set',mode:'resource',source:{kind:'plan',query:'fleet'}}},queries:{fleet:{object:asset,limit:1}}}}};
 f.target.definitions.push(dashboard);
 f.bindings.embeddings={wQuiver:{migration:'actual-analysis-page',page:{ref:dashboard.ref,sourceVersion:dashboard.version},contentVersion:dashboard.contentVersion,interfaceVersion:0,ports:{}}};
 return f;
}
