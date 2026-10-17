import type {Api} from "@platform/kernel";
export type RuleInput={name:string;type:string;choices?:string;required?:boolean;ref?:string;minLength?:number};
export type StateAction={from:string[];to?:string;toInput?:string;inputs?:RuleInput[];approval?:unknown;transaction?:Api.BusinessTxn};
const ruleChoices=(value:string|undefined)=>[...new Set((value??'').split(',').map(v=>v.trim()).filter(Boolean))];
/** Canvas edges are a view of the subject's original transaction assignment. */
const stateAssignment=(action:StateAction)=>action.transaction?.steps.find(step=>step.kind==="update"&&step.target?.type===action.transaction?.subject&&step.target?.id==="$subject")?.changes?.find(change=>change.field==="state");
export function actionStateInput(action:StateAction):string|undefined {
 const assignment=stateAssignment(action),ref=assignment?.expr?.ref??assignment?.from;
 if(ref&&!ref.startsWith("=")&&action.inputs?.some(input=>input.name===ref.replace(/^input\./,"")))return ref.replace(/^input\./,"");
 return action.transaction?undefined:action.toInput;
}
export function actionDestinations(action:StateAction):string[]{
 const assignment=stateAssignment(action),input=actionStateInput(action),literal=assignment?.expr?.lit?.value??(assignment?.from?.startsWith("=")?assignment.from.slice(1):undefined);
 return input?ruleChoices(action.inputs?.find(candidate=>candidate.name===input)?.choices):typeof literal==="string"?[literal]:!action.transaction&&action.to?[action.to]:action.from;
}
export function actionResultEdges(action:StateAction):string[]{return stateAssignment(action)||!action.transaction&&(action.toInput||action.to)?actionDestinations(action):[];}
export function stateInputValid(action:StateAction,states:readonly {name:string}[]):boolean {
 const name=actionStateInput(action);if(!name)return true;const input=action.inputs?.find(input=>input.name===name);
 return input?.type==="choice"&&input.required===true&&ruleChoices(input.choices).length>0&&ruleChoices(input.choices).every(value=>states.some(state=>state.name===value))&&(!action.transaction?!action.to&&!action.approval:true);
}
export function setActionDestination<A extends StateAction>(action:A,value:string|undefined,subject:string):A {
 const transaction=action.transaction??{subject,steps:[]},index=transaction.steps.findIndex(step=>step.kind==="update"&&step.target?.type===subject&&step.target.id==="$subject");
 const assignment:Api.TxnSet={field:"state",expr:{op:"",kind:"choice",lit:{value:value??""}}};
 const steps=transaction.steps.map((step,at)=>at===index?{...step,changes:[...(step.changes??[]).filter(change=>change.field!=="state"),...(value?[assignment]:[])]}:step);
 if(index<0&&value)steps.push({name:"state",kind:"update",target:{type:subject,id:"$subject"},changes:[assignment]});
 return {...action,to:undefined,toInput:undefined,transaction:{...transaction,steps}};
}
export function ruleInputValid(i:RuleInput,entities:readonly {type:string}[]):boolean{return (i.type==='reference'?!!i.ref&&!i.choices&&entities.some(e=>e.type===i.ref):!i.ref)&&(i.minLength===undefined||['text','longtext'].includes(i.type)&&Number.isInteger(i.minLength)&&i.minLength>=0&&i.minLength<=4096);}
