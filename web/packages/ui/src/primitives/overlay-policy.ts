/** Both overlay frames share explicit pointer and keyboard dismissal rules. */
export type OverlayPolicy={backdrop?:boolean;closeOnBackdrop?:boolean;closeOnEsc?:boolean};
export function overlayPolicy({backdrop=true,closeOnBackdrop=true,closeOnEsc=true}:OverlayPolicy){
 return {onEscapeKeyDown:(event:{preventDefault:()=>void})=>{if(!closeOnEsc)event.preventDefault();},onInteractOutside:(event:{preventDefault:()=>void})=>{if(!backdrop||!closeOnBackdrop)event.preventDefault();}};
}
