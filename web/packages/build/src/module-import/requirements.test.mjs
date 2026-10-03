import assert from "node:assert/strict";
import test from "node:test";
import {workshopRequirements} from "./requirements.ts";

test("TagList exposes its property mapping from the actual source object set even without table columns",()=>{
 const module={variables:[{id:"assets",type:"objectSet",objectSet:{objectType:"Asset",steps:[]}},{id:"staff",type:"objectSet",sourceObjectType:"Person"}],widgets:{tags:{type:"TagList",config:{objectSetVarId:"assets",property:"model"}},privateTags:{type:"TagList",config:{objectSetVarId:"assets",property:"secret"}},staffTags:{type:"TagList",config:{objectSetVarId:"staff",property:"role"}},steps:{type:"Stepper",config:{steps:["Observe"],currentVarId:"index"}},tabs:{type:"Tabs",config:{tabs:["First"],variableId:"index"}}}};
 const needs=workshopRequirements(module);assert.deepEqual(needs.objects,["Asset","Person"]);assert.deepEqual([...needs.fields.Asset],["model","secret"]);assert.deepEqual([...needs.fields.Person],["role"]);assert.deepEqual(needs.actions,[]);
 const noGuess=workshopRequirements({variables:module.variables,widgets:{tags:{type:"TagList",config:{objectSetVarId:"missing",property:"secret"}}}});assert.equal(noGuess.fields.Asset,undefined);assert.equal(noGuess.fields.Person,undefined);
});
