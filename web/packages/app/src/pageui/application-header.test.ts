import {expect,test} from "vitest";
import type {Api} from "@platform/kernel";
import {validApplicationHeader} from "./application-header";
const header = ():Api.ApplicationHeader => ({variant:"horizontal",title:"Original",items:[{kind:"logo"},{kind:"title"},{kind:"tabs",pages:["two","one"]},{kind:"button",label:"Refresh",action:"refresh"},{kind:"button",label:"Theme",action:"theme"}]});
test("the original application header supports its navigation and appearance controls",()=>{
 expect(validApplicationHeader(header(),["one","two"])).toBe(true);
 for(const change of [(h:Api.ApplicationHeader)=>{h.variant="unknown";},(h:Api.ApplicationHeader)=>{h.collapsed=true;},(h:Api.ApplicationHeader)=>{h.logo="javascript:alert(1)";},(h:Api.ApplicationHeader)=>{h.items[2]!.pages=["missing"];},(h:Api.ApplicationHeader)=>{h.items[2]!.pages=["one","one"];},(h:Api.ApplicationHeader)=>{h.items[3]!.action="execute";},(h:Api.ApplicationHeader)=>{h.items[0]!.kind="unknown";}]){
  const next=header();change(next);expect(validApplicationHeader(next,["one","two"])).toBe(false);
 }
});
