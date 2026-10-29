# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: model-driven-ui.spec.ts >> model permissions mark a partial collection and disable an unwritable cell with its owner reason
- Location: tests/model-driven-ui.spec.ts:147:1

# Error details

```
Test timeout of 90000ms exceeded.
```

```
Error: locator.click: Test timeout of 90000ms exceeded.
Call log:
  - waiting for getByRole('button', { name: 'Edit cells', exact: true })

```

# Page snapshot

```yaml
- generic [ref=f1e2]:
  - generic [ref=f1e3]:
    - complementary "Platform" [ref=f1e4]:
      - button "Home" [ref=f1e5]
      - button "Search" [ref=f1e9]
      - button "Notifications" [ref=f1e13]:
        - generic [ref=f1e17]: "5"
      - button "Applications" [ref=f1e18]
      - button "Recent" [ref=f1e22]
      - button "Favorites" [ref=f1e26]
      - generic [ref=f1e29]:
        - button "Assistant" [ref=f1e30]
        - button "manager-1 · browser-primary" [ref=f1e34]:
          - generic [ref=f1e35]: M
    - banner [ref=f1e36]:
      - button "Toggle navigation" [expanded] [ref=f1e37]
      - button "Switch application" [ref=f1e40]:
        - generic [ref=f1e45]: Builder
      - button "Add to favorites" [ref=f1e48]
      - menubar [ref=f1e51]:
        - menuitem "View" [ref=f1e52]
        - menuitem "Appearance" [ref=f1e53]
        - menuitem "Window" [ref=f1e54]
      - button "Search and commands" [ref=f1e55]:
        - generic [ref=f1e60]: ⌘K
      - generic [ref=f1e63]:
        - button "Offline applications" [ref=f1e64]
        - generic [ref=f1e65]: browser-primary
        - button "Last activated release" [ref=f1e66]:
          - text: "Last activated release:"
          - code [ref=f1e67]: 02091aaa31
    - generic [ref=f1e68]:
      - navigation "Main" [ref=f1e69]:
        - generic [ref=f1e70]:
          - generic [ref=f1e71]: Applications
          - button "All applications" [ref=f1e72]
        - generic [ref=f1e76]:
          - generic [ref=f1e77]: Model
          - button "Object types" [ref=f1e78]
          - button "Link types" [ref=f1e90]
          - button "Shared properties" [ref=f1e95]
          - button "Queries" [ref=f1e101]
        - generic [ref=f1e106]:
          - generic [ref=f1e107]: Behavior
          - button "Object behavior" [ref=f1e108]
          - button "Logic flows" [ref=f1e114]
          - button "Automations" [ref=f1e120]
          - button "AI functions" [ref=f1e126]
          - button "Agents" [ref=f1e131]
          - button "Code functions" [ref=f1e136]
          - button "Decision tables" [ref=f1e141]
          - button "Alert rules" [ref=f1e145]
        - generic [ref=f1e152]:
          - generic [ref=f1e153]: Interface
          - button "Pages" [ref=f1e154]
          - button "Templates" [ref=f1e160]
        - generic [ref=f1e166]:
          - generic [ref=f1e167]: Connections
          - button "Connections" [ref=f1e168]
          - button "Data sources" [ref=f1e173]
          - button "Datasets" [ref=f1e179]
          - button "Pipelines" [ref=f1e185]
          - button "Writebacks" [ref=f1e191]
          - button "Matching rules" [ref=f1e196]
          - button "Observation sources" [ref=f1e202]
          - button "Integration health" [ref=f1e208]
        - generic [ref=f1e214]:
          - generic [ref=f1e215]: Release
          - button "Sample sets" [ref=f1e216]
          - button "Evaluation sets" [ref=f1e218]
          - button "Business test plans" [ref=f1e223]
          - button "Changes" [ref=f1e229]
          - button "Release review" [ref=f1e235]
          - button "Release history" [ref=f1e241]
      - generic [ref=f1e250]:
        - generic [ref=f1e260]:
          - region "Release review" [ref=f1e267]:
            - tablist [ref=f1e270]:
              - tab "Page · Scoped items" [ref=f1e271] [cursor=pointer]
              - tab "Release review" [selected] [ref=f1e276] [cursor=pointer]:
                - generic [ref=f1e279]:
                  - generic [ref=f1e280]: Release review
                  - button "Close tab" [ref=f1e281]
            - tabpanel "Release review" [ref=f1e285]
          - status [ref=f1e286]: Release review opened
          - alert [ref=f1e287]
        - region "Release review" [ref=f1e290]:
          - group [ref=f1e291]:
            - generic [ref=f1e292]:
              - generic [ref=f1e295]:
                - heading "Release review" [level=1] [ref=f1e296]
                - button "About Release review" [ref=f1e297]: View help
              - generic "Saved releases" [ref=f1e301]:
                - generic [ref=f1e302]:
                  - heading "Saved releases" [level=2] [ref=f1e303]
                  - button "Refresh releases" [ref=f1e304]
                - paragraph [ref=f1e305]:
                  - text: "Current active release:"
                  - code [ref=f1e306]: sha256-v1:02091aaa317aec61dd6a562c08aa1211a6792108aff5a3a97f8351052417f3ce
                - generic [ref=f1e307]:
                  - text: Saved candidate
                  - combobox "Saved candidate" [ref=f1e308]:
                    - option "Choose a saved candidate"
                    - option "Scoped item · 3 assets · 2417f3ce · Active" [selected]
                    - option "Country · 4 assets · 9f69420d"
                    - option "Change discovery app · 2 assets · da47648c"
                    - option "Country · 4 assets · 8664f03b"
                    - option "Runtime case application · 3 assets · df6fc1d0"
                    - option "Master data extension · 45 assets · a6f94a6b"
                    - option "Versioned order · 6 assets · 32c74ee3"
                    - option "Business route · 5 assets · bf9a68f9"
                    - option "Service visits · 13 assets · 31b649d2"
                    - option "Service application · 8 assets · 621b9a77"
                    - option "Shared context application · 4 assets · a1e9ec0f"
                    - option "Device dispatch operations · 10 assets · 3748fc31"
                    - option "Heating control · 5 assets · fe029941"
                    - option "Preview boundary application · 3 assets · 31ff4ba6"
                    - option "Operation · 4 assets · 4f0edf4c"
                    - option "Experience application · 4 assets · dee886eb"
                    - option "Change discovery app · 6 assets · 372850ef"
                    - option "Scoped item · 3 assets · 8ea7d833"
                    - option "Shared service resources · 4 assets · 0f3a9d85"
                    - option "Transaction balance · 9 assets · e2851e0e"
                - generic [ref=f1e309]:
                  - generic [ref=f1e310]: Showing 20 of 20 candidates
                  - button "Previous" [disabled]
                  - button "Next" [disabled]
                  - button "Review active release" [ref=f1e311]
              - generic [ref=f1e312]:
                - generic [ref=f1e313]:
                  - text: Definition kind
                  - combobox "Definition kind" [ref=f1e314]:
                    - option "Sample sets"
                    - option "Object types"
                    - option "Link types"
                    - option "Shared properties"
                    - option "Queries"
                    - option "Pages" [selected]
                    - option "Logic flows"
                    - option "AI functions"
                    - option "Evaluation sets"
                    - option "Business test plans"
                    - option "Agents"
                    - option "Code functions"
                    - option "Decision tables"
                    - option "Alert rules"
                    - option "Connections"
                    - option "Observation sources"
                    - option "Data sources"
                    - option "Datasets"
                    - option "Pipelines"
                    - option "Writebacks"
                    - option "Matching rules"
                    - option "Applications"
                - generic [ref=f1e315]:
                  - text: Saved draft
                  - combobox "Saved draft" [ref=f1e316]:
                    - option "Choose a saved draft"
                    - option "Visit desk · published"
                    - option "Empty page · draft"
                    - option "Preview boundaries · published"
                    - option "New page of published application · draft"
                    - option "Embedded preview · draft"
                    - option "First context page · published"
                    - option "Second context page · published"
                    - option "Retirement draft 065 · draft"
                    - option "Record workspace · published"
                    - option "Reference page · published"
                    - option "Master data extension · published"
                    - option "Service desk · published"
                    - option "Dependency consumer · published"
                    - option "Source-edited page · draft"
                    - option "Receiving workspace · draft"
                    - option "Original query binding · draft"
                    - option "Equipment allocation · published"
                    - option "Orders · published"
                    - option "Scoped items · published" [selected]
                - button "Check draft and dependencies" [ref=f1e317]
              - generic "Deliver drafts together" [ref=f1e318]:
                - generic [ref=f1e319]:
                  - heading "Deliver drafts together" [level=2] [ref=f1e320]
                  - button "About Deliver drafts together" [ref=f1e321]: View help
                - generic [ref=f1e325]:
                  - button "Add selected draft" [ref=f1e326]
                  - button "Add the drafts it depends on" [ref=f1e327]
                  - button "Clear selection" [ref=f1e328]
                - list "Joint selection" [ref=f1e329]:
                  - listitem [ref=f1e330]:
                    - generic [ref=f1e331]:
                      - text: Pages ·
                      - code [ref=f1e332]: PAGE-MVTIE6UW66
                    - button "Remove" [ref=f1e333]
                - button "Check joint candidate" [ref=f1e334]
              - generic [ref=f1e335]:
                - generic [ref=f1e336]:
                  - generic [ref=f1e337]: Saved candidate review
                  - generic [ref=f1e338]: Active
                - button "Candidate identity" [ref=f1e341]
                - generic [ref=f1e344]:
                  - button "Promote to another environment…" [ref=f1e345]
                  - button "About Promote to another environment…" [ref=f1e346]: View help
                - button "Sealed definitions · 3" [ref=f1e351]
                - status [ref=f1e355]: Running definitions match this saved release.
                - generic [ref=f1e356]:
                  - generic [ref=f1e357]:
                    - heading "Added · 0" [level=3] [ref=f1e358]
                    - list
                  - generic [ref=f1e359]:
                    - heading "Changed · 0" [level=3] [ref=f1e360]
                    - list
                  - generic [ref=f1e361]:
                    - heading "Removed · 0" [level=3] [ref=f1e362]
                    - list
                - generic [ref=f1e363]:
                  - heading "Included assets · 3" [level=3] [ref=f1e364]
                  - list [ref=f1e365]:
                    - listitem [ref=f1e366]: build/action/build.objmvtie66e56.edit
                    - listitem [ref=f1e367]: build/object/build.objmvtie66e56
                    - listitem [ref=f1e368]: build/page/pagemvtie6uw65
                - generic [ref=f1e369]:
                  - button "Activate release" [disabled]
                  - status [ref=f1e370]:
                    - text: Release active for operators.
                    - code [ref=f1e371]: sha256-v1:02091aaa317aec61dd6a562c08aa1211a6792108aff5a3a97f8351052417f3ce
  - region "Notifications alt+T"
```

# Test source

```ts
  85  |   await row.getByRole('button',supplierReference).click();
  86  |   const supplierInspection=tab.getByRole('dialog',{name:supplierID!,exact:true});
  87  |   await expect(supplierInspection.getByRole('heading',{name:'Primary supplier',exact:true})).toBeVisible();
  88  |   await supplierInspection.getByRole('button',{name:'Close',exact:true}).click();
  89  |   await expect(row.getByRole('button',supplierReference)).toBeFocused();
  90  |   await details.getByRole('button',supplierReference).click();
  91  |   const countryReference=supplierInspection.getByRole('button',{name:`${country.type}/${countryID}`,exact:true});
  92  |   await expect(countryReference).toHaveText('Canada');
  93  |   await countryReference.click();
  94  |   const countryInspection=tab.getByRole('dialog',{name:countryID!,exact:true});
  95  |   await expect(countryInspection.getByRole('heading',{name:'Canada',exact:true})).toBeVisible();
  96  |   await countryInspection.getByRole('button',{name:'Close',exact:true}).click();
  97  |   await expect(tab.getByRole('dialog')).toHaveCount(0);
  98  |   expect(tab.url()).toBe(beforeInspection);
  99  |   await expect(search).toHaveValue('Order A');
  100 |   await expect(row).toHaveAttribute('aria-selected','true');
  101 |   await expect(details.getByRole('heading',{name:'Order A',exact:true})).toBeVisible();
  102 |   await editor.open(builder,draft.id);
  103 |   await editor.select('Orders');
  104 |   await editor.inspector.getByRole('tab',{name:'Data',exact:true}).click();
  105 |   const model=editor.inspector.getByLabel('Model binding',{exact:true});
  106 |   await model.getByRole('button',{name:'Use model field defaults',exact:true}).click();
  107 |   await expect(model.getByText('Inferred from model',{exact:true})).toBeVisible();
  108 |   await editor.save();
  109 |   await expect.poll(async()=> (await builder.sections(draft.id))[0].model?.type).toBe(order.type);
  110 |   const migrated=await builder.record('build.page',draft.id);
  111 |   const [presentation]=migrated.widgetSpec.body.sections;
  112 |   expect(presentation.object).toBeUndefined();
  113 |   expect(presentation.fields).toBeUndefined();
  114 |   expect(migrated.dataBindings[0].model.type).toBe(order.type);
  115 |   expect(migrated.widgetSpec.body.document.uiProfile).toBe(pageUIProfile);
  116 |   await editor.release();
  117 |   await tab.reload();
  118 |   await expect(tab.getByRole('cell',{name:'Order A',exact:true})).toBeVisible();
  119 |   await editor.open(builder,draft.id);
  120 |   await editor.select('Orders');
  121 |   await editor.inspector.getByRole('tab',{name:'Data',exact:true}).click();
  122 |   await model.getByRole('button',{name:'Add model field',exact:true}).click();
  123 |   const field=model.getByLabel('Declared model path',{exact:true}).last();
  124 |   await field.getByRole('combobox',{name:'Model field',exact:true}).selectOption('supplier');
  125 |   await expect(field.getByRole('combobox',{name:'Model field',exact:true})).toHaveValue('supplier');
  126 |   await field.getByRole('button',{name:'Follow declared reference',exact:true}).click();
  127 |   await field.getByRole('combobox',{name:'Reference hop 1',exact:true}).selectOption('country');
  128 |   await expect(field.getByRole('combobox',{name:'Reference hop 1',exact:true})).toHaveValue('country');
  129 |   await field.getByRole('button',{name:'Follow declared reference',exact:true}).click();
  130 |   await field.getByRole('combobox',{name:'Reference hop 2',exact:true}).selectOption('name');
  131 |   await expect(field.getByRole('combobox',{name:'Reference hop 2',exact:true})).toHaveValue('name');
  132 |   await editor.save();
  133 |   await expect.poll(async()=> (await builder.sections(draft.id))[0].model.fields?.[0]?.path).toEqual(['supplier','country','name']);
  134 |   await editor.release();
  135 |   await tab.reload();
  136 |   await expect(tab.getByRole('cell',{name:'Canada',exact:true})).toBeVisible();
  137 |   await shots(tab,tab.locator('body'),'model-path',testInfo);
  138 | 	// Fail an actual declared hop after a successful read: old relationship
  139 | 	// content must disappear and the owning component reports the failure.
  140 | 	await tab.route(`**/v1/records/${supplier.type}/*`,route=>route.abort('failed'));
  141 | 	await tab.reload();
  142 | 	await expect(tab.getByText('Model binding could not be read. Check its declared source and permissions.',{exact:true})).toBeVisible();
  143 | 	await expect(tab.getByRole('cell',{name:'Canada',exact:true})).toHaveCount(0);
  144 |   await tab.close();
  145 | });
  146 | 
  147 | test('model permissions mark a partial collection and disable an unwritable cell with its owner reason',async({page,builder,operator,editor},testInfo)=>{
  148 |   test.setTimeout(90_000);
  149 |   const object=await builder.object({title:'Scoped item',fields:[{name:'name',title:'Item name',type:'text'},{name:'note',title:'Protected note',type:'text',write:['builder']}],access:[{role:'user',read:'own',create:true,edit:true}]});
  150 |   await builder.records(object.type,[{name:'Hidden item',note:'Hidden note'}]);
  151 |   const visible=fresh('VISIBLE');
  152 |   await operator.decide(`${object.type}.create`,{type:object.type,id:visible},{name:'Visible item'});
  153 |   await builder.decide(`${object.type}.edit`,{type:object.type,id:visible},{note:'Original protected note'});
  154 |   const sections=[{id:'items',widget:'table',configVersion:1,title:'Scoped items',model:{type:object.type,visibility:'mark'},inlineEdit:{action:`${object.type}.edit`,fields:['name','note']}}];
  155 |   const document={formatVersion:2,uiProfile:'platform.page.v2.110',root:'root',nodes:{root:{kind:'rows',children:['items']},items:{kind:'widget',section:'items'}}};
  156 |   const draft=await builder.page({title:'Scoped items',object:object.type,actions:[`${object.type}.edit`],sections,document});
  157 |   await editor.open(builder,draft.id);
  158 |   await editor.select('Scoped items');
  159 |   await editor.inspector.getByRole('tab',{name:'Data',exact:true}).click();
  160 |   const inherit=editor.inspector.getByRole('checkbox',{name:'Inherit page actions',exact:true});
  161 |   await expect(inherit).toBeChecked();
  162 |   await inherit.uncheck();
  163 |   await editor.save();
  164 |   await expect.poll(async()=>(await builder.sections(draft.id))[0].actions).toEqual([]);
  165 |   await page.reload();
  166 |   await editor.select('Scoped items');
  167 |   await editor.inspector.getByRole('tab',{name:'Data',exact:true}).click();
  168 |   await expect(inherit).not.toBeChecked();
  169 |   await editor.release();
  170 |   const tab=await runtime(page,operator,draft.name);
  171 |   await expect(tab.getByRole('cell',{name:'Visible item',exact:true})).toBeVisible();
  172 |   await expect(tab.getByRole('button',{name:'Edit cells',exact:true})).toHaveCount(0);
  173 |   await editor.open(builder,draft.id);
  174 |   await editor.select('Scoped items');
  175 |   await editor.inspector.getByRole('tab',{name:'Data',exact:true}).click();
  176 |   await inherit.check();
  177 |   await editor.save();
  178 |   await expect.poll(async()=>(await builder.sections(draft.id))[0].actions).toBeUndefined();
  179 |   await editor.release();
  180 |   await tab.reload();
  181 |   await expect(tab.getByText('Some records or fields are outside your permissions.',{exact:true})).toBeVisible();
  182 |   await expect(tab.getByRole('cell',{name:'Visible item',exact:true})).toBeVisible();
  183 |   await expect(tab.getByRole('cell',{name:'Hidden item',exact:true})).toHaveCount(0);
  184 |   await expect(tab.getByRole('button',{name:'Import CSV',exact:true})).toHaveCount(0);
> 185 |   await tab.getByRole('button',{name:'Edit cells',exact:true}).click();
      |                                                                ^ Error: locator.click: Test timeout of 90000ms exceeded.
  186 |   const note=tab.getByRole('cell',{name:'Original protected note',exact:true});
  187 |   await expect(note).toHaveAttribute('aria-disabled','true');
  188 |   await expect(note).toHaveAttribute('title',/outside.*permissions/);
  189 |   await note.dblclick();
  190 |   await expect(note.getByRole('textbox')).toHaveCount(0);
  191 |   await shots(tab,tab.locator('body'),'model-permissions',testInfo);
  192 |   await builder.edit(draft.id,{sections:[{...sections[0],model:{type:object.type}}]});
  193 |   await builder.decide('build.page.publish',{type:'build.page',id:draft.id},{});
  194 |   await tab.reload();
  195 |   await expect(tab.getByRole('table')).toHaveCount(0);
  196 |   await tab.close();
  197 | });
  198 | 
```