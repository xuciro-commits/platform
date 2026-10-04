export type RuleInput={name:string;type:string;choices?:string;required?:boolean;ref?:string;minLength?:number};
export type StateAction={from:string[];to?:string;toInput?:string;inputs?:RuleInput[];approval?:unknown};
export const ruleChoices=(value:string|undefined)=>[...new Set((value??'').split(',').map(v=>v.trim()).filter(Boolean))];
/** The original declaration owns destinations; canvas gestures cannot rewrite choice inputs. */
export function actionDestinations(a:StateAction):string[]{return a.toInput?ruleChoices(a.inputs?.find(i=>i.name===a.toInput)?.choices):a.to?[a.to]:a.from;}
export function actionResultEdges(a:StateAction):string[]{return a.toInput||a.to?actionDestinations(a):[];}
export function stateInputValid(a:StateAction,states:readonly {name:string}[]):boolean{if(!a.toInput)return true;const i=a.inputs?.find(i=>i.name===a.toInput);return !a.to&&!a.approval&&i?.type==='choice'&&i.required===true&&ruleChoices(i.choices).length>0&&ruleChoices(i.choices).every(v=>states.some(s=>s.name===v));}
export function ruleInputValid(i:RuleInput,entities:readonly {type:string}[]):boolean{return (i.type==='reference'?!!i.ref&&!i.choices&&entities.some(e=>e.type===i.ref):!i.ref)&&(i.minLength===undefined||['text','longtext'].includes(i.type)&&Number.isInteger(i.minLength)&&i.minLength>=0&&i.minLength<=4096);}
export function assignmentInputFits(i:RuleInput,f:{type:string;ref?:string},legacyReference=false):boolean{return f.type==='reference'?(i.type==='reference'&&i.ref===f.ref||legacyReference&&['text','longtext','choice'].includes(i.type)):['text','longtext'].includes(f.type)?['text','longtext','choice'].includes(i.type):f.type==='decimal'?['decimal','integer'].includes(i.type):i.type===f.type;}
