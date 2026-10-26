import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { CanvasEditor, type CanvasModel } from './CanvasEditor';
import { ViewVisibilityContext } from '../shell/ViewVisibility';
import { setLanguage } from '../i18n';

let frames:Map<number,FrameRequestCallback>,nextFrame:number,resize:ResizeObserverCallback,disconnect:ReturnType<typeof vi.fn>;
beforeEach(()=>{
 setLanguage('en');frames=new Map();nextFrame=0;disconnect=vi.fn();
 vi.stubGlobal('requestAnimationFrame',(callback:FrameRequestCallback)=>{frames.set(++nextFrame,callback);return nextFrame;});
 vi.stubGlobal('cancelAnimationFrame',(id:number)=>frames.delete(id));
 vi.stubGlobal('ResizeObserver',class {constructor(callback:ResizeObserverCallback){resize=callback;}observe(){}disconnect=disconnect;});
});
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals();});
const model:CanvasModel={root:{label:'Page',kind:'container',children:['widget'],horizontal:false},widget:{label:'Widget',kind:'widget',parent:'root',children:[],horizontal:false}};
function Example({visible=true,disabled=false}:{visible?:boolean;disabled?:boolean}){
 return <ViewVisibilityContext.Provider value={visible}><CanvasEditor model={model} selected="widget" disabled={disabled} onSelect={()=>{}} onDrop={()=>{}} onMove={()=>{}} resize={()=>undefined} onResize={()=>{}} onResetSize={()=>{}}>
  <div data-canvas-scroll><div data-canvas-node="widget">Widget content</div></div>
 </CanvasEditor></ViewVisibilityContext.Provider>;
}
function flush(){act(()=>{const pending=[...frames.values()];frames.clear();pending.forEach(callback=>callback(0));});}

test('a stationary selection stops measuring and follows layout or scroll changes',()=>{
 let top=100;
 const measure=vi.spyOn(HTMLElement.prototype,'getBoundingClientRect').mockImplementation(function(){return {x:0,y:top,left:0,top,right:200,bottom:top+100,width:200,height:100,toJSON(){}};});
 const {container}=render(<Example/>);
 flush();
 expect(screen.getByRole('toolbar',{name:'Canvas selection tools'}).style.top).toBe('100px');
 const count=measure.mock.calls.length;
 expect(frames.size).toBe(0);
 flush();
 expect(measure.mock.calls.length).toBe(count);
 top=140;
 act(()=>resize([],{} as ResizeObserver));
 fireEvent.scroll(container.querySelector('[data-canvas-scroll]')!);
 expect(frames.size).toBe(1);
 flush();
 expect(screen.getByRole('toolbar',{name:'Canvas selection tools'}).style.top).toBe('140px');
 expect(frames.size).toBe(0);
});

test('hidden and disabled canvases release selection observers and resume when visible',()=>{
 vi.spyOn(HTMLElement.prototype,'getBoundingClientRect').mockReturnValue({x:0,y:100,left:0,top:100,right:200,bottom:200,width:200,height:100,toJSON(){}});
 const {rerender}=render(<Example/>);flush();
 expect(screen.getByRole('toolbar',{name:'Canvas selection tools'})).toBeTruthy();
 rerender(<Example visible={false}/>);
 expect(screen.queryByRole('toolbar',{name:'Canvas selection tools'})).toBeNull();
 expect(disconnect).toHaveBeenCalled();
 expect(frames.size).toBe(0);
 rerender(<Example/>);flush();
 expect(screen.getByRole('toolbar',{name:'Canvas selection tools'})).toBeTruthy();
 rerender(<Example disabled/>);
 expect(screen.queryByRole('toolbar',{name:'Canvas selection tools'})).toBeNull();
 expect(frames.size).toBe(0);
});
