import {expect,runtime,shots,stableReadRevision,test,type Builder} from './kit';
import {professionalConfigurations,workstationConfiguration} from '../../packages/app/src/composites/configurations';

/** Instantiates the actual Catalog asset through the original builder kit.
 * Only model and record identities change between isolated test applications. */
async function instantiate(builder:Builder,name:keyof typeof professionalConfigurations){
 const example=professionalConfigurations[name],types=new Map<string,string>(),ids=new Map<string,Map<string,string>>(),pending=new Set(Object.keys(example.sources));
 while(pending.size){
  const next=[...pending].find(key=>example.sources[key]!.fields.every(f=>!f.ref||types.has(f.ref)));
  if(!next)throw Error('Configuration model dependency cycle');
  const source=example.sources[next]!,model=await builder.object({title:next,fields:source.fields.map(f=>({...f,ref:f.ref?types.get(f.ref):undefined}))});
  types.set(next,model.type);const records=new Map<string,string>();ids.set(next,records);
  for(const row of source.records){const values={...row};delete values.id;for(const f of source.fields)if(f.ref&&values[f.name])values[f.name]=ids.get(f.ref)?.get(String(values[f.name]));const [id]=await builder.records(model.type,[values]);records.set(String(row.id),id!);}
  pending.delete(next);
 }
 const config=structuredClone(example.page);
 config.object={app:'build',kind:'object',name:types.get(config.object.name)!};
 for(const section of config.sections!)for(const binding of Object.values(section.composite!.bindings))binding.model.type=types.get(binding.model.type)!;
 const draft=await builder.page({title:config.title,object:config.object.name,sections:config.sections,document:config.document});
 return {draft,config};
}

test('one configured graph presents routing, organization and document lineage',async({page,builder,operator,editor},testInfo)=>{
 test.setTimeout(180_000);
 await stableReadRevision(page);
 for(const name of ['routing','organization','lineage'] as const){
  const {draft,config}=await instantiate(builder,name);
  await editor.open(builder,draft.id);
  await editor.select(config.title);
  await editor.inspector.getByRole('tab',{name:'Data',exact:true}).click();
  await expect(editor.inspector.getByLabel('Composite model bindings',{exact:true})).toBeVisible();
  await editor.release();
  const tab=await runtime(page,operator,draft.name);
  const graph=tab.locator('.react-flow');
  if(name==='routing'){
   await expect(graph.getByText('20 Assemble',{exact:true})).toBeVisible();
   await expect(graph.getByText(/Assembly · NC DIM-01 · hold/)).toBeVisible();
   await expect(graph.locator('.platform-flow-current')).toHaveCount(1);
   await graph.getByText('20 Assemble',{exact:true}).click();
   await expect(tab.getByRole('button',{name:'Open record',exact:true})).toBeVisible();
  }else if(name==='organization'){
   await expect(graph.getByText('Customer services',{exact:true})).toBeVisible();
   await expect(graph.getByText('2026-01 to 2026-12 · effective',{exact:true})).toBeVisible();
  }else await expect(graph.getByText('Reviewed 2',{exact:true})).toBeVisible();
  await shots(tab,tab.locator('body'),`professional-${name}`,testInfo);
  await tab.setViewportSize({width:390,height:844});
  await expect(tab.getByRole('columnheader',{name:'Node',exact:true})).toBeVisible();
  await tab.close();
 }
});

test('the same matrix displays host capability and rate expressions',async({page,builder,operator,editor},testInfo)=>{
 test.setTimeout(120_000);
 await stableReadRevision(page);
 for(const name of ['capability','rates'] as const){
  const {draft}=await instantiate(builder,name);
  await editor.open(builder,draft.id);
  await editor.release();
  const tab=await runtime(page,operator,draft.name);
  if(name==='capability'){
   const row=tab.getByRole('row').filter({has:tab.getByRole('cell',{name:'Drill B',exact:true})});
   await expect(row.getByRole('cell',{name:'No',exact:true})).toBeVisible();
   await expect(row.getByRole('cell',{name:'Yes',exact:true})).toBeVisible();
  }else{
   const row=tab.getByRole('row').filter({has:tab.getByRole('cell',{name:'Member',exact:true})});
   await expect(row.getByRole('cell',{name:'80',exact:true})).toBeVisible();
   await expect(row.getByRole('cell',{name:'40',exact:true})).toBeVisible();
  }
  await shots(tab,tab.locator('body'),`professional-${name}`,testInfo);
  await tab.close();
 }
});

test('a configured workstation combines typed reporting, state and original eligible resources',async({page,builder,operator,editor},testInfo)=>{
 test.setTimeout(120_000);
 await stableReadRevision(page);
 const task=await builder.object({title:'Task',fields:[{name:'name',title:'Name',type:'text',search:true},{name:'minimum',title:'Minimum',type:'decimal'}],states:[{name:'ready',title:'Ready'},{name:'done',title:'Done'}],actions:[{name:'report',title:'Report',from:['ready'],to:'done',inputs:[{name:'measurement',title:'Measurement',type:'decimal',required:true},{name:'passed',title:'Passed',type:'boolean',required:true}]}]});
 const condition={op:'>=',kind:'boolean',args:[{op:'',kind:'decimal',ref:'capacity'},{op:'path',kind:'decimal',args:[{op:'',kind:'reference',ref:'input.task'}],path:['minimum']}]};
 const resource=await builder.object({title:'Resource',fields:[{name:'name',title:'Name',type:'text',search:true},{name:'capacity',title:'Capacity',type:'decimal'}],states:[{name:'ready',title:'Ready'},{name:'active',title:'Active'}],actions:[{name:'start',title:'Start',from:['ready'],to:'active',inputs:[{name:'task',title:'Task',type:'reference',ref:task.type,required:true}],conditions:[{expr:condition,message:'Insufficient capacity'}]}]});
 const [taskID]=await builder.records(task.type,[{name:'Task A',minimum:100}]);
 await builder.records(resource.type,[{name:'Press A',capacity:120},{name:'Drill B',capacity:40}]);
 const config=structuredClone(workstationConfiguration);
 for(const section of config.sections!){if(section.model!.type==='factory.task')section.model!.type=task.type;else {section.model!.type=resource.type;section.model!.options!.rule!.action={app:'build',kind:'action',name:`${resource.type}.start`};}if(section.actions)section.actions=[{app:'build',kind:'action',name:`${task.type}.report`}];}
 const draft=await builder.page({title:'Workstation',object:task.type,sections:config.sections!.map(s=>({...s,actions:s.actions?.map(a=>a.name)})),document:config.document});
 await editor.open(builder,draft.id);
 await editor.release();
 const tab=await runtime(page,operator,draft.name);
 await tab.getByRole('row').filter({hasText:'Task A'}).click();
 await expect(tab.getByRole('combobox',{name:'Eligible resources',exact:true})).toBeVisible({timeout:15000});
 await tab.getByRole('combobox',{name:'Eligible resources',exact:true}).click();
 await expect(tab.getByRole('option',{name:/ · Press A$/})).toBeVisible();
 await expect(tab.getByRole('option',{name:/ · Drill B$/})).toHaveCount(0);
 await tab.keyboard.press('Escape');
 await tab.getByRole('spinbutton',{name:'Measurement *',exact:true}).fill('105.5');
 await tab.getByRole('checkbox',{name:'Passed *',exact:true}).check();
 await shots(tab,tab.locator('body'),'professional-workstation',testInfo);
 await tab.getByRole('button',{name:'Report',exact:true}).click();
 await expect.poll(async()=>(await operator.record(task.type,taskID!)).state).toBe('done');
 await tab.close();
});
