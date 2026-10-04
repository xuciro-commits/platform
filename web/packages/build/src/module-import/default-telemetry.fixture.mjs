import {defaultMaintenanceFixture} from './default-maintenance.fixture.mjs';
/** Complete original Telemetry page, with actual sample fields and an authorized GLB attachment. */
export function defaultTelemetryFixture(profile){
 const f=defaultMaintenanceFixture(profile),asset={app:'build',kind:'object',name:f.target.object},sample={app:'build',kind:'object',name:'build.reading'},query=name=>({ref:{app:'build',kind:'query',name},sourceVersion:'1.query-1'}),all=query('plant-readings'),history=query('asset-readings');
 f.bindings.regions={sTelemetryRoot:{maxHeight:960},sDrawerRoot:{maxHeight:640}};
 const known={0:['vibration','mm/s'],10:['temperature','C'],20:['pressure','bar'],60:['speed','rpm'],70:['current','A']},signals=Array.from({length:100},(_,i)=>({sourceIndex:i,field:known[i]?.[0]??`signal${i}`,unit:known[i]?.[1]??'',group:'Plant observations'}));
 f.target.entities.push({app:sample.app,type:sample.name,fields:[{name:'at',type:'datetime'},{name:'asset',type:'reference',ref:asset.name},{name:'plant',type:'text'},{name:'device',type:'text'},{name:'state',type:'choice',choices:['normal','warning']},...signals.map(s=>({name:s.field,type:'decimal'}))]});
 for(const [binding,by] of [[all,undefined],[history,'asset']])f.target.definitions.push({ref:binding.ref,version:binding.sourceVersion,query:{object:sample.name,by,sort:['-at','id']}});
 const base={migration:'actual-business-observations',sampleQuery:all,timeField:'at',signals,assetField:'asset'};
 f.bindings.observations={wLiveTable:{...base,assetConsumers:'observation-table',metadata:{plant:'plant',device:'device',status:'state'}},wLiveStats:{...base,contextQuery:history}};
 f.bindings.spatial={wDigitalTwin:{migration:'original-spatial',fileID:'robot-cell-model',sampleQuery:history,sampleAssetField:'asset',sampleTimeField:'at',mappingFields:Object.fromEntries(f.module.widgets.wDigitalTwin.config.mappings.map(m=>[m.id,m.source==='telemetry'?signals[m.signalIndex].field:'openAlerts']))}};
 return f;
}
