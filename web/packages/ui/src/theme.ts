import {useSyncExternalStore} from "react";

const listeners=new Set<()=>void>();
const media=()=>typeof matchMedia==="function"?matchMedia("(prefers-color-scheme: dark)"):undefined;
const snapshot=():"light"|"dark"=>typeof document==="undefined"?"light":document.documentElement.dataset.theme==="dark"?"dark":document.documentElement.dataset.theme==="light"?"light":media()?.matches?"dark":"light";
const publish=()=>listeners.forEach(fn=>fn());
const subscribe=(fn:()=>void)=>{listeners.add(fn);const query=media();query?.addEventListener("change",fn);return ()=>{listeners.delete(fn);query?.removeEventListener("change",fn);};};
/** Appearance belongs to the shared UI token owner; application definitions
 * never carry CSS, storage keys or changes to the document element. */
export function useTheme(){const theme=useSyncExternalStore(subscribe,snapshot,()=>"light" as const);return {theme,toggle:()=>{document.documentElement.dataset.theme=theme==="light"?"dark":"light";publish();}};}
