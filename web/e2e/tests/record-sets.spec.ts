import { expect, runtime, slug, test } from "./kit";

test("builder publishes an ordered set and an operator splits its records atomically", async ({page,builder,operator,editor}) => {
  const access=[{role:"user",read:"all",create:true,edit:true,archive:true}];
  const name=slug("batch");
  const batch=await builder.object({name,title:"Batch",fields:[{name:"label",title:"Label",type:"text"},{name:"consumed",title:"Consumed",type:"boolean"}],states:[{name:"draft",title:"Draft"}],access,
    actions:[{name:"split",title:"Split batch",from:["draft"],roles:["user"],transaction:{subject:`build.${name}`,steps:[{name:"consume",kind:"update",target:{type:`build.${name}`,id:"$subject"},changes:[{field:"consumed",from:"=true"}]}]}}],
  } as any);
  const line=await builder.object({name:slug("line"),title:"Batch line",fields:[{name:"batch",title:"Batch",type:"reference",ref:batch.type,inverse:"lines",required:true},{name:"ordinal",title:"Ordinal",type:"integer",required:true}],access} as any);
  await builder.open(page,`/object-type?id=${batch.id}&tab=sets`);
  await page.getByRole("button",{name:"Add a record set",exact:true}).click();
  await page.getByRole("textbox",{name:"Set name",exact:true}).fill("lines");
  await page.getByRole("textbox",{name:"Stable order · JSON",exact:true}).fill(JSON.stringify([{expr:{kind:"integer",ref:"ordinal"}}],null,2));
  await editor.release();
  await expect.poll(async()=>JSON.parse((await builder.record("build.object",batch.id)).published).sets?.[0]?.name).toBe("lines");
  const compare=(op:string)=>({op,kind:"boolean",args:[{kind:"integer",ref:"row.ordinal"},{kind:"integer",lit:{value:2}}]});
  const transaction={subject:batch.type,steps:[
    {name:"left",kind:"create",target:{type:batch.type},changes:[{field:"label",from:"=Left batch"},{field:"consumed",from:"=false"}]},
    {name:"right",kind:"create",target:{type:batch.type},changes:[{field:"label",from:"=Right batch"},{field:"consumed",from:"=false"}]},
    {name:"splitlines",kind:"for-each",forEach:{set:"lines",steps:[
      {name:"assignleft",kind:"update",target:{type:line.type,id:"$item"},when:compare("<="),changes:[{field:"batch",from:"step.left"}]},
      {name:"assignright",kind:"update",target:{type:line.type,id:"$item"},when:compare(">"),changes:[{field:"batch",from:"step.right"}]},
    ]}},
    {name:"consume",kind:"update",target:{type:batch.type,id:"$subject"},changes:[{field:"consumed",from:"=true"}]},
  ]};
  await builder.open(page,`/action-type?id=${batch.id}&action=split`);
  await page.getByRole("button",{name:"Transaction",exact:true}).click();
  const input=page.getByRole("textbox",{name:"Transaction definition",exact:true});
  await input.fill("{");
  await expect(page.getByText("Enter valid JSON. The original draft is retained.",{exact:true})).toBeVisible();
  await input.fill(JSON.stringify(transaction,null,2));
  await editor.release();
  await expect.poll(async()=>JSON.parse((await builder.record("build.object",batch.id)).published).actions[0].transaction.steps[2]?.kind).toBe("for-each");
  const saved=JSON.parse((await builder.record("build.object",batch.id)).published);
  expect(saved.actions[0].transaction.steps[2]).toMatchObject({kind:"for-each",forEach:{set:"lines"}});
  const [root]=await builder.records(batch.type,[{label:"Input batch",consumed:false}]);
  const ids=await builder.records(line.type,[4,1,3,2].map(ordinal=>({batch:root,ordinal})));
  const tab=await runtime(page,operator,batch.name);
  await tab.getByText("Input batch",{exact:true}).first().click();
  await tab.getByRole("button",{name:"Split batch",exact:true}).click();
  await expect.poll(async()=>(await builder.record(batch.type,root!)).consumed).toBe(true);
  const rows=await Promise.all(ids.map(id=>builder.record(line.type,id)));
  const groups=new Map<string,number[]>();
  for(const row of rows)groups.set(row.batch,[...groups.get(row.batch)??[],row.ordinal]);
  expect([...groups.values()].map(values=>values.sort()).sort((a,b)=>a[0]!-b[0]!)).toEqual([[1,2],[3,4]]);
  for(const [batchID,ordinals] of groups) {
    const response=await operator.request.post(`/v1/aggregates/${line.type}/query`,{headers:operator.headers,data:{collection:{object:batch.type,id:batchID,name:"lines"},measures:["count","sum:ordinal"]}});
    expect(response.ok()).toBe(true);
    const aggregate=await response.json();
    expect(aggregate.rows[0]).toMatchObject({count:2,"sum:ordinal":ordinals.reduce((sum,value)=>sum+value,0)});
    expect(aggregate.partial??false).toBe(false);
  }
});
