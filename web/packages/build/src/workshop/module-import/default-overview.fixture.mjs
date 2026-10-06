import {defaultOperationsFixture} from './default-operations.fixture.mjs';
/** Same complete capture and original application; explicit Overview metadata only. */
export function defaultOverviewFixture(profile){
 const f=defaultOperationsFixture(profile);
 f.bindings.regions={sOverviewRoot:{maxHeight:960},sDrawerRoot:{maxHeight:640}};
 f.bindings.unusedConfigurations={wUnused1:{migration:'complete-unused-configuration',collection:'allAssets',xProperty:'pressure',yProperty:'temperature',colorBy:'status',labelField:'name'},wUnused2:{migration:'complete-unused-configuration',collection:'allAssets',columns:['name']}};
 f.bindings.objects.Alert='build.alert';f.target.entities[0].fields.push({name:'exposure',type:'integer'});
 f.bindings.aggregates.revenueAtRisk={measure:'sum:exposure'};f.bindings.lists={wObjectList1:{label:'name',fields:['state','priority']}};
 const binding={ref:{app:'build',kind:'query',name:'open-alerts'},sourceVersion:'1.query-1'};
 f.bindings.queries.activeAlerts=binding;f.target.definitions.push({ref:binding.ref,version:binding.sourceVersion,query:{object:'build.alert',domain:[['state','=','open']],limit:100}});
 return f;
}
