import {createElement,lazy,type ComponentType} from 'react';
import {widgetContract,type WidgetID} from './registry';

/** The binding adapter sees owner context; the lazy renderer sees only its ports. */
export function defineWidgetPlugin<Context>(){
 return function<Props extends object>(id:WidgetID,configVersion:number,bind:(context:Context)=>Props,load:()=>Promise<ComponentType<Props>>,key?:(context:Context)=>string){
  const contract=widgetContract(id);
  if(!contract||contract.configVersion!==configVersion)throw new Error(`Unsupported widget plugin: ${id}/${configVersion}`);
  const Renderer=lazy(async()=>({default:await load()}));
  const Adapter=(context:Context)=>createElement(Renderer,{...bind(context),...(key?{key:key(context)}:{})});
  return Object.freeze({contract,configVersion,bind,Renderer:Adapter});
 };
}
