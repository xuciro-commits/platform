import {expect,test} from "vitest";
import {boundedGLB} from "./glb";
import {triangleGLB} from "./scene-fixture";
import {validSceneConfig,type SceneConfig} from "./scene-model";
import {validExternalFrame} from "../components/external-frame";
import {validStaticImage} from "../components/static-image";

const scene=():SceneConfig=>({background:"light",quality:"balanced",showGrid:false,layers:[{id:"layer",name:"Equipment",nodes:["FixturePart"],visible:false,wireframe:false,opacity:0}],mappings:[{id:"travel",node:"FixturePart",field:"position",source:"asset",mode:"position",axis:"x",inputMin:0,inputMax:100,outputMin:0,outputMax:1,enabled:false}]});
test("explicit false and zero scene values survive presentation validation",()=>{const value=scene(),original=JSON.stringify(value);expect(validSceneConfig(value)).toBe(true);expect(JSON.stringify(value)).toBe(original);});
test.each([
 (value:SceneConfig)=>{value.layers.push({...value.layers[0]!});},
 (value:SceneConfig)=>{value.layers[0]!.nodes=["FixturePart","FixturePart"];},
 (value:SceneConfig)=>{value.layers[0]!.opacity=1.01;},
 (value:SceneConfig)=>{value.mappings[0]!.inputMax=0;},
 (value:SceneConfig)=>{value.mappings[0]!.outputMax=Infinity;},
 (value:SceneConfig)=>{value.mappings.push({...value.mappings[0]!,id:"other"});},
 (value:SceneConfig)=>{value.mappings[0]!.colorHigh="red";},
])( "scene identity and visual drive declarations reject invalid configurations",change=>{const value=scene();change(value);expect(validSceneConfig(value)).toBe(false);});

test("the original GLB reader accepts a self-contained catalog model",()=>{expect(boundedGLB(triangleGLB())).toMatchObject({vertices:3,indices:0,nodes:1});});
test.each([
 (doc:Record<string,any>)=>{doc.buffers[0].uri="https://example.test/model.bin";},
 (doc:Record<string,any>)=>{doc.nodes[0].children=[0];},
 (doc:Record<string,any>)=>{doc.nodes.push({children:[1]});},
 (doc:Record<string,any>)=>{doc.accessors[0].count=250001;},
 (doc:Record<string,any>)=>{doc.extensionsRequired=["REMOTE_execution"];},
])( "GLB allocation and URI checks stay with the original renderer",change=>{expect(()=>boundedGLB(triangleGLB(change))).toThrow("bounded self-contained GLB");});

test.each(["http://docs.example.test/report","https://user:secret@docs.example.test/report","https://docs.example.test.evil/report","https://docs.example.test/../secret","https://docs.example.test/%2e%2e/secret","javascript:alert(1)","https://docs.example.test/with space","https://docs.example.test/中"])("an ambiguous external resource is refused without URL repair",url=>{expect(validExternalFrame({url,origin:"https://docs.example.test"})).toBe(false);});
test("resource presentation keeps explicit placeholders, zero heights and exact reviewed origins",()=>{
 expect(validStaticImage({url:"",caption:"",height:0})).toBe(true);
 expect(validStaticImage({url:"/image.png",height:0.5})).toBe(true);
 expect(validExternalFrame({url:"https://docs.example.test/report",origin:"https://docs.example.test",height:240})).toBe(true);
 expect(validExternalFrame({url:"https://docs.example.test/report",origin:"https://docs.example.test",height:0})).toBe(false);
});
