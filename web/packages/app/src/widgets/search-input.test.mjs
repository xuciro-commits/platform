import assert from "node:assert/strict";
import test from "node:test";
import {searchInputObjects} from "./search-input.ts";
test("search scope comes from same-owner original query search consumers and retires with missing bindings",()=>{
 const d={variables:{term:{scope:"page",type:"string",mode:"state"}},queries:{one:{object:{name:"sample.asset"},search:{variable:"term"}},two:{object:{name:"sample.asset"},search:{variable:"term"}},three:{object:{name:"sample.order"},search:{variable:"term"}},other:{owner:"overlay",object:{name:"hidden"},search:{variable:"term"}}}};assert.deepEqual(searchInputObjects(d,"term"),["sample.asset","sample.order"]);d.variables.term.scope="overlay";d.variables.term.owner="overlay";assert.deepEqual(searchInputObjects(d,"term"),["hidden"]);d.variables.term.mode="constant";assert.deepEqual(searchInputObjects(d,"term"),[]);assert.deepEqual(searchInputObjects(undefined,"term"),[]);
});
