import type {Api} from '@platform/kernel';

type Source={type:string;fields:Api.FieldInfo[];records:Record<string,unknown>[]};
type Configuration={page:Api.Page;sources:Record<string,Source>;answer:Api.PageCompositeResult};
const field=(name:string,type:Api.FieldInfo['type']='text',ref?:string):Api.FieldInfo=>({name,title:name,type,...name==='name'?{search:true}:{},...ref?{ref}:{}});
const view=(type:string,roles:Record<string,Api.FieldRef>={}):Api.ModelViewBinding=>({model:{type},roles});
const ref=(name:string):Api.FieldRef=>({name});
function page(name:string,title:string,object:string,sections:Api.Section[]):Api.Page{
 return {name,title,object:{app:object.split('.')[0]!,kind:'object',name:object},layout:'composed',listFields:[],detailFields:[],actions:[],sections,document:{formatVersion:2,uiProfile:'platform.page.v2.111',root:'root',nodes:{root:{kind:'rows',children:sections.map(s=>s.id!)},...Object.fromEntries(sections.map(s=>[s.id!,{kind:'widget',section:s.id}]))},variables:{}}};
}

// These are portable application configuration assets, not component code.
// Offline answers are explicit synthetic samples; live reads use the host.
const routingConfiguration:Configuration={
 page:page('routing','Routing','factory.step',[{id:'graph',widget:'layered-graph',configVersion:1,title:'Routing',composite:{acyclic:true,tones:{active:'info',hold:'warning',done:'success',scrapped:'danger'},bindings:{nodes:view('factory.step',{layer:ref('center'),detail:{path:['center','name']},order:ref('number')}),layers:view('factory.center'),edges:view('factory.link',{from:ref('from'),to:ref('to'),dashed:ref('dashed'),state:ref('status')}),overlay:view('factory.position',{node:ref('step'),current:ref('current'),state:ref('status'),detail:ref('detail')})}}}]),
 sources:{
  'factory.center':{type:'factory.center',fields:[field('name')],records:[{id:'cutting',name:'Cutting'},{id:'assembly',name:'Assembly'}]},
  'factory.step':{type:'factory.step',fields:[field('name'),field('number','integer'),field('center','reference','factory.center')],records:[{id:'10',name:'10 Cut',number:10,center:'cutting'},{id:'20',name:'20 Assemble',number:20,center:'assembly'},{id:'end',name:'Done',number:30,center:'assembly'}]},
  'factory.link':{type:'factory.link',fields:[field('from','reference','factory.step'),field('to','reference','factory.step'),field('dashed','boolean'),field('status')],records:[{id:'10-20',from:'10',to:'20',dashed:false,status:''},{id:'20-end',from:'20',to:'end',dashed:false,status:''}]},
  'factory.position':{type:'factory.position',fields:[field('step','reference','factory.step'),field('current','boolean'),field('status'),field('detail')],records:[{id:'previous',step:'10',current:false,status:'done',detail:''},{id:'here',step:'20',current:true,status:'hold',detail:'NC DIM-01'}]},
 },
 answer:{sources:{nodes:{object:'factory.step',records:[{id:'10','$view.label':'10 Cut','$view.layer':'cutting','$view.detail':'Cutting','$view.order':10},{id:'20','$view.label':'20 Assemble','$view.layer':'assembly','$view.detail':'Assembly','$view.order':20},{id:'end','$view.label':'Done','$view.layer':'assembly','$view.order':30}]},layers:{object:'factory.center',records:[{id:'cutting','$view.label':'Cutting'},{id:'assembly','$view.label':'Assembly'}]},edges:{object:'factory.link',records:[{id:'10-20','$view.from':'10','$view.to':'20'},{id:'20-end','$view.from':'20','$view.to':'end'}]},overlay:{object:'factory.position',records:[{id:'previous','$view.node':'10','$view.state':'done'},{id:'here','$view.node':'20','$view.current':true,'$view.state':'hold','$view.detail':'NC DIM-01'}]}}},
};

const organizationConfiguration:Configuration={
 page:page('organization','Organization','directory.unit',[{id:'graph',widget:'layered-graph',configVersion:1,title:'Organization',composite:{direction:'down',acyclic:true,tones:{effective:'success'},bindings:{nodes:view('directory.unit',{layer:ref('division')}),layers:view('directory.division'),edges:view('directory.reporting',{from:ref('superior'),to:ref('subordinate'),label:ref('period')}),overlay:view('directory.tenure',{node:ref('unit'),detail:ref('period'),state:ref('status')})}}}]),
 sources:{
  'directory.division':{type:'directory.division',fields:[field('name')],records:[{id:'leadership',name:'Leadership'},{id:'delivery',name:'Delivery'}]},
  'directory.unit':{type:'directory.unit',fields:[field('name'),field('division','reference','directory.division')],records:[{id:'board',name:'Board',division:'leadership'},{id:'service',name:'Customer services',division:'delivery'}]},
  'directory.reporting':{type:'directory.reporting',fields:[field('superior','reference','directory.unit'),field('subordinate','reference','directory.unit'),field('period')],records:[{id:'reports',superior:'board',subordinate:'service',period:'2026-01 to 2026-12'}]},
  'directory.tenure':{type:'directory.tenure',fields:[field('unit','reference','directory.unit'),field('period'),field('status')],records:[{id:'tenure',unit:'service',period:'2026-01 to 2026-12',status:'effective'}]},
 },
 answer:{sources:{nodes:{object:'directory.unit',records:[{id:'board','$view.label':'Board','$view.layer':'leadership'},{id:'service','$view.label':'Customer services','$view.layer':'delivery'}]},layers:{object:'directory.division',records:[{id:'leadership','$view.label':'Leadership'},{id:'delivery','$view.label':'Delivery'}]},edges:{object:'directory.reporting',records:[{id:'reports','$view.from':'board','$view.to':'service','$view.label':'2026-01 to 2026-12'}]},overlay:{object:'directory.tenure',records:[{id:'tenure','$view.node':'service','$view.detail':'2026-01 to 2026-12','$view.state':'effective'}]}}},
};

const path=(root:string,name:string,kind:string):Api.Expr=>({op:'path',kind,args:[{op:'',kind:'reference',ref:`input.${root}`}],path:[name]});
const capabilityMatrixConfiguration:Configuration={
 page:page('capabilities','Capability matrix','factory.resource',[{id:'matrix',widget:'model-matrix',configVersion:1,title:'Capability matrix',composite:{bindings:{rows:view('factory.resource'),columns:view('factory.requirement')},value:{op:'>=',kind:'boolean',args:[path('row','capacity','decimal'),path('column','minimum','decimal')]},tones:{true:'success',false:'warning'}}}]),
 sources:{'factory.resource':{type:'factory.resource',fields:[field('name'),field('capacity','decimal')],records:[{id:'press',name:'Press A',capacity:120},{id:'drill',name:'Drill B',capacity:40}]},'factory.requirement':{type:'factory.requirement',fields:[field('name'),field('minimum','decimal')],records:[{id:'light',name:'Light duty',minimum:30},{id:'heavy',name:'Heavy duty',minimum:100}]}},
 answer:{sources:{rows:{object:'factory.resource',records:[{id:'press','$view.label':'Press A'},{id:'drill','$view.label':'Drill B'}]},columns:{object:'factory.requirement',records:[{id:'light','$view.label':'Light duty'},{id:'heavy','$view.label':'Heavy duty'}]}},cells:[{row:'press',column:'light',value:true,kind:'boolean'},{row:'press',column:'heavy',value:true,kind:'boolean'},{row:'drill',column:'light',value:true,kind:'boolean'},{row:'drill',column:'heavy',value:false,kind:'boolean'}]},
};
const rateMatrixConfiguration:Configuration={
 page:page('rates','Rate matrix','service.plan',[{id:'matrix',widget:'model-matrix',configVersion:1,title:'Rate matrix',composite:{bindings:{rows:view('service.plan'),columns:view('service.charge')},value:{op:'*',kind:'decimal',args:[path('row','factor','decimal'),path('column','amount','decimal')]}}}]),
 sources:{'service.plan':{type:'service.plan',fields:[field('name'),field('factor','decimal')],records:[{id:'standard',name:'Standard',factor:1},{id:'member',name:'Member',factor:0.8}]},'service.charge':{type:'service.charge',fields:[field('name'),field('amount','decimal')],records:[{id:'visit',name:'Visit',amount:100},{id:'support',name:'Support',amount:50}]}},
 answer:{sources:{rows:{object:'service.plan',records:[{id:'standard','$view.label':'Standard'},{id:'member','$view.label':'Member'}]},columns:{object:'service.charge',records:[{id:'visit','$view.label':'Visit'},{id:'support','$view.label':'Support'}]}},cells:[{row:'standard',column:'visit',value:'100',kind:'decimal'},{row:'standard',column:'support',value:'50',kind:'decimal'},{row:'member',column:'visit',value:'80',kind:'decimal'},{row:'member',column:'support',value:'40',kind:'decimal'}]},
};

const lineageConfiguration:Configuration={
 page:page('lineage','Document lineage','archive.version',[{id:'graph',widget:'layered-graph',configVersion:1,title:'Document lineage',composite:{acyclic:true,bindings:{nodes:view('archive.version'),edges:view('archive.predecessor',{from:ref('previous'),to:ref('next')})}}}]),
 sources:{'archive.version':{type:'archive.version',fields:[field('name')],records:[{id:'v1',name:'Draft 1'},{id:'v2',name:'Reviewed 2'}]},'archive.predecessor':{type:'archive.predecessor',fields:[field('previous','reference','archive.version'),field('next','reference','archive.version')],records:[{id:'revision',previous:'v1',next:'v2'}]}},
 answer:{sources:{nodes:{object:'archive.version',records:[{id:'v1','$view.label':'Draft 1'},{id:'v2','$view.label':'Reviewed 2'}]},edges:{object:'archive.predecessor',records:[{id:'revision','$view.from':'v1','$view.to':'v2'}]}}},
};

export const workstationConfiguration=page('desk','Workstation','factory.task',[
 {id:'tasks',widget:'table',configVersion:1,title:'Current tasks',model:{type:'factory.task'}},
 {id:'parameters',widget:'detail',configVersion:1,title:'Typed parameters',model:{type:'factory.task'}},
 {id:'resources',widget:'record-picker',configVersion:1,title:'Eligible resources',model:{type:'factory.resource',options:{rule:{action:{app:'factory',kind:'action',name:'factory.resource.start'},inputs:{task:'task'}}}}},
 {id:'status',widget:'status-tracker',configVersion:1,title:'Current status',model:{type:'factory.task'}},
 {id:'report',widget:'inline-action',configVersion:1,title:'Report',model:{type:'factory.task'},actions:[{app:'factory',kind:'action',name:'factory.task.report'}]},
]);
workstationConfiguration.document!.variables={task:{scope:'page',type:'record',mode:'resource',source:{kind:'record',section:'tasks'}}};

export const professionalConfigurations={routing:routingConfiguration,organization:organizationConfiguration,capability:capabilityMatrixConfiguration,rates:rateMatrixConfiguration,lineage:lineageConfiguration};
