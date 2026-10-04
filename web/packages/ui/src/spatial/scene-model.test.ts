import {expect,test} from "vitest";
import {boundedGLB} from "./glb";
import {triangleGLB} from "./scene-fixture";
import {sceneLimits,sceneMappingValue,validSceneConfig,type SceneMapping,type SceneConfig} from "./scene-model";
import type {EntityInfo,EntityRecord} from "../records/Records";
const record=(id:string,data:Record<string,unknown>)=>({id,...data}) as EntityRecord,info=(type:string,fields:unknown[])=>({type,fields}) as EntityInfo;
const mapping:SceneMapping={id:"drive",node:"FixturePart",source:"sample",field:"signal",mode:"position",axis:"x",inputMin:0,inputMax:20,outputMin:-1,outputMax:1,enabled:true};
test("a real GLB admits bounded core geometry and rejects URI, decoder, prototype, graph and allocation escapes before loading",()=>{
 expect(boundedGLB(triangleGLB()).nodes).toBe(1);
 for(const mutate of [(d:Record<string,any>)=>d.buffers[0].uri="https://outside.example/model.bin",(d:Record<string,any>)=>d.extensionsRequired=["KHR_draco_mesh_compression"],(d:Record<string,any>)=>d.nodes[0].children=[0],(d:Record<string,any>)=>d.accessors[0].count=sceneLimits.maxVertices+1,(d:Record<string,any>)=>d.bufferViews[0].byteLength=9999,(d:Record<string,any>)=>d.nodes[0].extras={constructor:"outside"}])expect(()=>boundedGLB(triangleGLB(mutate))).toThrow();
 const truncated=triangleGLB().slice(0,-4);expect(()=>boundedGLB(truncated)).toThrow();const bad=triangleGLB(),end=new DataView(bad);end.setFloat32(bad.byteLength-4,NaN,true);expect(()=>boundedGLB(bad)).toThrow();
});
test("scene mappings use actual typed values and a sample's original asset reference, never defaults or numeric coercion",()=>{
 const asset={record:record("A",{pressure:10}),info:info("sample.asset",[{name:"pressure",type:"decimal"}])},sample={record:record("S",{asset:"A",signal:15}),info:info("sample.observation",[{name:"asset",type:"reference",ref:"sample.asset"},{name:"signal",type:"decimal"}])};expect(sceneMappingValue(mapping,asset,sample,"asset")).toEqual({raw:15,normalised:.75,value:.5});sample.record.asset="B";expect(sceneMappingValue(mapping,asset,sample,"asset")).toBeUndefined();sample.record.asset="A";sample.record.signal="15";expect(sceneMappingValue(mapping,asset,sample,"asset")).toBeUndefined();expect(sceneMappingValue({...mapping,source:"asset",field:"pressure"},asset)).toEqual({raw:10,normalised:.5,value:0});
});
test("fixed scene configuration rejects duplicate drivers and nonfinite or excessive ranges",()=>{
 const config:SceneConfig={background:"dark",showGrid:true,quality:"balanced",layers:[],mappings:[mapping]};expect(validSceneConfig(config)).toBe(true);expect(validSceneConfig({...config,mappings:[mapping,{...mapping,id:"other"}]})).toBe(false);expect(validSceneConfig({...config,mappings:[{...mapping,inputMax:NaN}]})).toBe(false);expect(validSceneConfig({...config,mappings:[null as unknown as SceneMapping]})).toBe(false);
});
