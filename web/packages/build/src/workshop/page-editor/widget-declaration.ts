import {pageUIManifest, type Api} from '@platform/kernel';
import {t} from '@platform/ui';
import {schemaIssue} from '../../automate/workflow-schema';

type Configuration = Record<string, unknown>;
const propertyNames = new Set(pageUIManifest.widgets.flatMap(widget=>Object.keys(widget.propsSchema.properties)));
const portNames = new Set(pageUIManifest.widgets.flatMap(widget=>[...widget.inputPorts,...widget.outputPorts].map(port=>port.bindingField)).filter(field=>field!=='enabledWhen'));
const atPath = (record: unknown,path: string): unknown => path.split('.').reduce<unknown>((value,key)=>value&&typeof value==='object'?(value as Configuration)[key]:undefined,record);

/** All authoring modes meet the legacy flat Section here. Only properties
 * owned by the shared manifest are projected; model/action validation stays in
 * the existing page owner and never copies or reinterprets a business model. */
export function widgetDeclarationIssues(value: Configuration): Api.FieldIssue[] {
  if (!Array.isArray(value.sections)) return [];
  return value.sections.flatMap((section: unknown,index): Api.FieldIssue[]=>{
    if(!section || typeof section!=='object' || Array.isArray(section))return [];
    const record=section as Configuration,widget=pageUIManifest.widgets.find(item=>item.componentID===record.widget);
    const issue=(path:string[],message:string):Api.FieldIssue=>({path:['sections',String(index),...path],code:'schema',message});
    const unsupported=t('Unsupported widget configuration: {widget}, version {version}. Your page draft is preserved.',{widget:String(record.widget),version:String(record.configVersion)});
    if(!widget)return [issue(['widget'],unsupported)];
    if(record.configVersion!==widget.configVersion)return [issue(['configVersion'],unsupported)];
    const declaredPorts = new Set<string>([...widget.inputPorts,...widget.outputPorts].map(port=>port.bindingField));
    for(const field of portNames){
      const binding=atPath(record,field);
      if(binding!==undefined&&binding!==''){
        if(!declaredPorts.has(field))return [issue(field.split('.'),t('Unknown field: {name}',{name:field}))];
        if(typeof binding!=='string')return [issue(field.split('.'),t('Expected text.'))];
      }
    }
    const properties: Configuration={};
    for(const [key,item] of Object.entries(record))if(item!==undefined&&propertyNames.has(key))properties[key]=item;
    const message=schemaIssue(widget.propsSchema as unknown as Api.ValueSchema,properties);
    return message?[issue([],message)]:[];
  });
}
