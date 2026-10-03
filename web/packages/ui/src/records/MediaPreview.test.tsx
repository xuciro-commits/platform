import {Blob as NodeBlob} from "node:buffer";
import {act,cleanup,render,screen,waitFor} from "@testing-library/react";
import {afterEach,beforeEach,expect,test,vi} from "vitest";
import {pageUIManifest} from "@platform/kernel";
import {MediaPreview} from "./MediaPreview";
import {rasterDimensions} from "./attachment-bytes";
import type {AttachedFile} from "./Records";
const png=Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNImHDhPwAF1ALAFmQ9YgAAAABJRU5ErkJggg=="),char=>char.charCodeAt(0)),stamp={by:"test",at:"2026-10-03T12:00:00Z"};
const blob=(bytes:Uint8Array=png,type="image/png")=>new NodeBlob([bytes as Uint8Array<ArrayBuffer>],{type}) as unknown as Blob;
const attachment=(data:Blob,id="FILE-1"):AttachedFile=>({id,revision:1,created:stamp,changed:stamp,by:"actual-member",name:"real.png",size:data.size,contentType:data.type});
const create=vi.fn(()=>"blob:authorized-raster"),revoke=vi.fn(),decode=vi.fn(),close=vi.fn();
beforeEach(()=>{create.mockClear();revoke.mockClear();decode.mockReset().mockResolvedValue({width:1,height:1,close});close.mockClear();vi.stubGlobal("createImageBitmap",decode);vi.stubGlobal("URL",Object.assign(class extends URL {},{createObjectURL:create,revokeObjectURL:revoke}));});
afterEach(()=>{cleanup();vi.unstubAllGlobals();});
test("authorized raster bytes decode before presentation and keep actual metadata and original download",async()=>{
 const data=blob(),download=vi.fn(),view=render(<MediaPreview attachment={attachment(data)} blob={data} scope="member:one" onDownload={download}/>);expect(screen.getByRole("status").textContent).toBe("Decoding authorized media bytes…");await screen.findByRole("img",{name:"real.png"});expect(decode).toHaveBeenCalledWith(data);expect(close).toHaveBeenCalledOnce();expect(create).toHaveBeenCalledWith(data);expect(screen.getByText("FILE-1 · image/png · 70 bytes")).toBeTruthy();fireDownload();expect(download).toHaveBeenCalledOnce();view.unmount();expect(revoke).toHaveBeenCalledWith("blob:authorized-raster");
 function fireDownload(){screen.getByRole("button",{name:"Download original attachment"}).click();}
});
test("scope and bytes changes remove old media immediately and revoke its object URL",async()=>{
 const data=blob(),view=render(<MediaPreview attachment={attachment(data)} blob={data} scope="member:one"/>);await screen.findByRole("img");view.rerender(<MediaPreview attachment={attachment(data)} blob={data} scope="member:two"/>);expect(screen.queryByRole("img")).toBeNull();expect(revoke).toHaveBeenCalledWith("blob:authorized-raster");await screen.findByRole("img");const other=blob();view.rerender(<MediaPreview attachment={attachment(other,"FILE-2")} blob={other} scope="member:two"/>);expect(screen.queryByRole("img")).toBeNull();await screen.findByRole("img");expect(decode).toHaveBeenCalledTimes(3);
});
test("a late decode from retired bytes cannot create or restore an object URL",async()=>{
 let resolve!:(decoded:{width:number;height:number;close:()=>void})=>void;decode.mockImplementationOnce(()=>new Promise(done=>{resolve=done;}));const data=blob(),view=render(<MediaPreview attachment={attachment(data)} blob={data} scope="old"/>);await waitFor(()=>expect(decode).toHaveBeenCalledOnce());const other=blob();view.rerender(<MediaPreview attachment={attachment(other,"NEW")} blob={other} scope="new"/>);await screen.findByRole("img");await act(async()=>resolve({width:1,height:1,close}));expect(create).toHaveBeenCalledOnce();expect(create).toHaveBeenCalledWith(other);expect(close).toHaveBeenCalledTimes(2);expect(screen.getByText("NEW · image/png · 70 bytes")).toBeTruthy();
});
test("unsupported executable media keeps only real metadata and download instead of loading its bytes",()=>{
 const data=blob(new TextEncoder().encode("<svg onload='bad()'></svg>"),"image/svg+xml"),file={...attachment(data),name:"original.svg"},view=render(<MediaPreview attachment={file} blob={data} onDownload={()=>{}}/>);expect(screen.getByRole("status").textContent).toContain("no inline media preview");expect(screen.getByText("original.svg")).toBeTruthy();expect(screen.queryByRole("img")).toBeNull();expect(decode).not.toHaveBeenCalled();expect(create).not.toHaveBeenCalled();expect(view.container.querySelector("svg,iframe,object")).toBeNull();
});
test("malformed types, metadata and oversized headers are rejected before decoding or allocating a preview URL",async()=>{
 const data=blob(),view=render(<MediaPreview attachment={{...attachment(data),size:0}} blob={data}/>);expect(screen.getByRole("alert").textContent).toContain("do not match");expect(decode).not.toHaveBeenCalled();
 const malformed=blob(new TextEncoder().encode("<svg></svg>"));view.rerender(<MediaPreview attachment={attachment(malformed)} blob={malformed}/>);await screen.findByRole("alert");expect(decode).not.toHaveBeenCalled();
 const wide=png.slice();new DataView(wide.buffer).setUint32(16,pageUIManifest.runtime.collaboration.maxImagePixels+1);const oversized=blob(wide);view.rerender(<MediaPreview attachment={attachment(oversized)} blob={oversized}/>);await waitFor(()=>expect(screen.getByRole("alert").textContent).toContain("dimensions exceed"));expect(decode).not.toHaveBeenCalled();expect(create).not.toHaveBeenCalled();
 const huge=blob(new Uint8Array(pageUIManifest.runtime.collaboration.maxPreviewBytes+1));view.rerender(<MediaPreview attachment={attachment(huge)} blob={huge}/>);expect(screen.getByRole("alert").textContent).toContain("bytes exceed");expect(decode).not.toHaveBeenCalled();
});
test("failed or inconsistent decoding never presents a misleading raster",async()=>{
 decode.mockRejectedValueOnce(new Error("Decoder rejected bytes"));const data=blob(),view=render(<MediaPreview attachment={attachment(data)} blob={data}/>);await screen.findByRole("alert");expect(screen.getByRole("alert").textContent).toBe("Raster bytes could not be decoded.");expect(screen.queryByRole("img")).toBeNull();expect(create).not.toHaveBeenCalled();
 decode.mockResolvedValueOnce({width:2,height:1,close});const other=blob();view.rerender(<MediaPreview attachment={attachment(other)} blob={other}/>);await waitFor(()=>expect(screen.getByRole("alert").textContent).toContain("dimensions do not match"));expect(close).toHaveBeenCalledOnce();expect(create).not.toHaveBeenCalled();
});
test("GIF, JPEG and WebP signatures retain exact declared dimensions and cannot substitute another media type",()=>{
 const gif=new Uint8Array(13);gif.set(new TextEncoder().encode("GIF89a"));gif[6]=2;gif[8]=3;expect(rasterDimensions(gif,"image/gif")).toEqual({width:2,height:3});expect(rasterDimensions(gif,"image/png")).toBeUndefined();
 const jpeg=Uint8Array.from([0xff,0xd8,0xff,0xc0,0,11,8,0,3,0,2,1,1,0x11,0]);expect(rasterDimensions(jpeg,"image/jpeg")).toEqual({width:2,height:3});expect(rasterDimensions(jpeg.subarray(0,9),"image/jpeg")).toBeUndefined();
 const webp=new Uint8Array(30);webp.set(new TextEncoder().encode("RIFF"));webp.set(new TextEncoder().encode("WEBPVP8X"),8);webp[24]=1;webp[27]=2;expect(rasterDimensions(webp,"image/webp")).toEqual({width:2,height:3});expect(rasterDimensions(webp,"image/jpeg")).toBeUndefined();
});
