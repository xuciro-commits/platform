// A search input read by a barcode scanner or a camera (ADR-0057 D1). Keyboard
// wedge scanners type the code and press Enter: the field keeps focus, commits
// on Enter and selects its text so the next scan replaces it. Where the browser
// has a BarcodeDetector, a camera button reads the code through the lens.
import {Button,Input,t} from '@platform/ui';
import {useEffect,useRef,useState} from 'react';

type Detector={detect(source:ImageBitmapSource):Promise<{rawValue:string}[]>};
const detectorOf=():Detector|undefined=>{const w=window as unknown as {BarcodeDetector?:new(o?:{formats?:string[]})=>Detector};return w.BarcodeDetector?new w.BarcodeDetector():undefined;};

export function ScanInput({value,onChange,disabled,label}:{value:string;onChange:(value:string)=>void;disabled?:boolean;label:string}){
 const field=useRef<HTMLInputElement>(null),video=useRef<HTMLVideoElement>(null);
 const [draft,setDraft]=useState(value),[camera,setCamera]=useState(false),[failure,setFailure]=useState('');
 useEffect(()=>{setDraft(value);},[value]);
 useEffect(()=>{if(!disabled)field.current?.focus();},[disabled]);
 const commit=(code:string)=>{const trimmed=code.trim();setDraft(trimmed);onChange(trimmed);requestAnimationFrame(()=>field.current?.select());};
 useEffect(()=>{
  if(!camera)return;
  const detector=detectorOf();let stream:MediaStream|undefined,stop=false,timer=0;
  (async()=>{
   try{stream=await navigator.mediaDevices.getUserMedia({video:{facingMode:'environment'}});if(stop||!video.current)return;video.current.srcObject=stream;await video.current.play();}
   catch{setFailure(t('The camera is not available.'));setCamera(false);return;}
   const tick=async()=>{if(stop||!video.current||!detector)return;try{const found=await detector.detect(video.current);const code=found[0]?.rawValue;if(code){commit(code);setCamera(false);return;}}catch{/* keep looking */}timer=window.setTimeout(tick,250);};
   void tick();
  })();
  return()=>{stop=true;window.clearTimeout(timer);stream?.getTracks().forEach(track=>track.stop());};
 },[camera]);
 return <div className="grid gap-2" data-scan-input>
  <div className="flex items-center gap-2">
   <Input ref={field} aria-label={label} className="font-mono text-lg" autoComplete="off" inputMode="text" enterKeyHint="go" placeholder={t('Scan or type a code')} value={draft} disabled={disabled} onChange={e=>setDraft(e.target.value)} onKeyDown={e=>{if(e.key==='Enter'){e.preventDefault();commit(draft);}}} onBlur={()=>{if(draft!==value)commit(draft);}}/>
   {value&&<Button variant="ghost" size="sm" disabled={disabled} onClick={()=>{commit('');field.current?.focus();}}>{t('Clear')}</Button>}
   {detectorOf()&&<Button variant={camera?'primary':'ghost'} size="sm" disabled={disabled} onClick={()=>{setFailure('');setCamera(on=>!on);}}>{t(camera?'Stop':'Camera')}</Button>}
  </div>
  {camera&&<video ref={video} muted playsInline className="aspect-video w-full rounded-md bg-black object-cover"/>}
  {failure&&<p role="alert" className="text-xs text-danger">{failure}</p>}
 </div>;
}
