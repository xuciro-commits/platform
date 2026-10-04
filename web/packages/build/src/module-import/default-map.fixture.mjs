import {defaultMaintenanceFixture} from './default-maintenance.fixture.mjs';
/** Full captured Map page, shared app, original overlays and unused inventory. */
export function defaultMapFixture(profile){
 const f=defaultMaintenanceFixture(profile);f.bindings.regions={sMapRoot:{maxHeight:960},sDrawerRoot:{maxHeight:640}};
 f.target.entities[0].fields.push({name:'latitude',type:'decimal'},{name:'longitude',type:'decimal'});
 const work=f.target.entities.find(e=>e.type==='build.work');work.fields.push({name:'started',type:'datetime'},{name:'finished',type:'datetime'});
 f.bindings.metricAnnotations=Object.fromEntries(['wKpi1~3','wKpi2~4','wKpi3~5'].map(id=>[id,{interpretation:'static-note'}]));
 f.bindings.spatial={wMap1:{migration:'original-spatial',latitudeField:'latitude',longitudeField:'longitude',labelField:'name'}};
 f.bindings.gantts={wGantt:{startField:'started',endField:'finished',titleField:'title',statusField:'status',rangeStart:'2026-10-01',rangeEnd:'2026-11-01',tones:[{value:'Open',tone:'warning'},{value:'Done',tone:'success'}]}};
 return f;
}
