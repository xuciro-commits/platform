import {loopOwner,overlayOwner,variableAccessible,type LayoutKind} from "../page-layout";
import {pageUIManifest,type Api} from "@platform/kernel";
import {widgetContract} from "@platform/app";
import {Button,Checkbox,Disclosure,Input,Select,InspectorField,InspectorSection,SegmentedChoice,t} from "@platform/ui";
import {AlignHorizontalJustifyStart,AlignHorizontalJustifyCenter,AlignHorizontalJustifyEnd,AlignHorizontalSpaceBetween,Columns2,Rows3,PanelTop,WrapText,ToolCase,Repeat,BetweenHorizontalStart,SquareDashed,RotateCcw} from "lucide-react";
type Label={id?:string;widget:string;title?:string};

export function LayoutProperties({ document, id, sections=[],onChange, onPatch, onUngroup,ungroupDisabled }: {
  document: Api.PageDocument; id: string; sections?:Label[];onChange: (kind: LayoutKind) => void; onPatch: (id: string, patch: Partial<Api.PageLayoutNode>) => void; onUngroup: () => void;ungroupDisabled?:boolean;
}) {
  const node = document.nodes[id];
  if (!node) return null;
  const parent=Object.values(document.nodes).find(n=>n.children?.includes(id)),owner=sections.find(s=>s.id===parent?.section),contract=owner?widgetContract(owner.widget):undefined,slot=contract&&"slots" in contract?contract.slots.find(s=>s.id===node.slot):undefined;
  return <div className="grid content-start">
    <InspectorSection title={t("Layout container")}>
    <SegmentedChoice compact label={t("Layout")} value={node.kind} options={([
      ["rows","Rows",Rows3],["columns","Columns",Columns2],["tabs","Tabs",PanelTop],["flow","Flow layout",WrapText],["toolbar","Toolbar",ToolCase],["loop","Loop",Repeat],
    ]as const).filter(([kind])=>!slot||(slot.allowedLayouts as readonly string[]).includes(kind)).map(([value,label,Icon])=>({value,label:t(label),icon:<Icon/>}))} onChange={kind=>onChange(kind as LayoutKind)}/>
    <label className="grid gap-1 text-xs">{t("Container title")}<Input value={node.title ?? ""} onChange={(event) => onPatch(id, { title: event.target.value })} /></label>
    {(node.kind === "flow" || node.kind === "toolbar") && <SegmentedChoice compact label={t("Alignment")} value={node.align??"start"} options={[
      {value:"start",label:t("Start"),icon:<AlignHorizontalJustifyStart/>},{value:"center",label:t("Center"),icon:<AlignHorizontalJustifyCenter/>},
      {value:"end",label:t("End"),icon:<AlignHorizontalJustifyEnd/>},{value:"between",label:t("Space between"),icon:<AlignHorizontalSpaceBetween/>},
    ]} onChange={align=>onPatch(id,{align})}/>}
    {node.kind === "loop" && node.loop && <>
      <label className="grid gap-1 text-xs">{t("Loop query window")}<Select value={node.loop.collection} onChange={(event) => onPatch(id, { loop: { ...node.loop!, collection: event.target.value } })}><option value="">{t("Choose a query window")}</option>{Object.entries(document.variables ?? {}).filter(([, value]) => (loopOwner(document,id)?value.scope==="loop-item"&&value.owner===loopOwner(document,id)&&value.source?.kind==="plan":variableAccessible(value,undefined,overlayOwner(document,id))) && value.type === "object-set" && (value.source?.kind === "query" || value.source?.kind === "plan" || value.mode==="shared")).map(([key, value]) => <option key={key} value={key}>{value.title || key}</option>)}</Select></label>
      <label className="grid gap-1 text-xs">{t("Loop item limit")}<Input type="number" min={1} max={100} value={node.loop.limit} onChange={(event) => onPatch(id, { loop: { ...node.loop!, limit: Number(event.target.value) } })} /></label>
      <p className="text-xs text-muted">{t("Record widgets bind to each item. The source query stays outside the loop.")}</p>
    </>}
    {node.kind === "tabs" && <>
      <label className="grid gap-1 text-xs">{t("Active tab variable")}<Select value={node.activeVariable ?? ""} onChange={(event) => onPatch(id, { activeVariable: event.target.value })}>
        {Object.entries(document.variables ?? {}).filter(([, value]) => value.type === "string" && value.mode === "state").map(([key, value]) => <option key={key} value={key}>{value.title || key}</option>)}
      </Select></label>
      {node.children?.map((child, i) => <label key={child} className="grid gap-1 text-xs">{t("Tab {n} title", { n: i + 1 })}<Input value={document.nodes[child]?.title ?? ""} onChange={(event) => onPatch(child, { title: event.target.value })} /></label>)}
      <p className="text-xs text-muted">{t("Tabs keep visited content mounted until this page session ends.")}</p>
    </>}
    <Button size="sm" variant="ghost" disabled={ungroupDisabled || !!node.slot || node.kind==="loop" || id === document.root || Object.values(document.overlays ?? {}).some((overlay) => overlay.root === id)} onClick={onUngroup}>{t("Ungroup")}</Button>
    </InspectorSection>
    {["rows","columns"].includes(node.kind)&&<InspectorSection title={t("Region presentation")} actions={<Button size="icon" variant="ghost" aria-label={t("Reset region presentation")} title={t("Reset region presentation")} onClick={()=>onPatch(id,{presentation:undefined})}><RotateCcw/></Button>}>
      <div className="grid grid-cols-2 gap-2">
        <InspectorField label={t("Region padding (px)")} prefix={<SquareDashed/>} unit="px" type="number" min={0} max={pageUIManifest.layout.maxPadding} step={1} value={node.presentation?.padding??""} placeholder="0" onChange={e=>onPatch(id,{presentation:{...node.presentation,padding:e.target.value===""?undefined:Number(e.target.value)}})}/>
        <Select aria-label={t("Region background")} className="text-xs" value={node.presentation?.background??"default"} onChange={e=>onPatch(id,{presentation:{...node.presentation,background:e.target.value}})}><option value="default">{t("Default")}</option><option value="panel">{t("Panel")}</option></Select>
      </div>
      <Disclosure summary={<span className="text-xs">{t("Border and header")}</span>} defaultOpen={!!node.presentation?.border||!!node.presentation?.showHeader}>
        <div className="grid gap-2 pt-2">{([['border','Region border'],['showHeader','Show region title'],['collapsible','Collapsible region'],['defaultCollapsed','Initially collapsed']]as const).map(([key,label])=><Checkbox key={key} className="text-xs" checked={!!node.presentation?.[key]} onChange={checked=>onPatch(id,{presentation:{...node.presentation,[key]:checked,...key==='showHeader'&&!checked?{collapsible:false,defaultCollapsed:false}:{},...key==='collapsible'?{...checked?{showHeader:true}:{defaultCollapsed:false}}:{}}})}>{t(label)}</Checkbox>)}</div>
      </Disclosure>
    </InspectorSection>}
  </div>;
}

/** Dimensions belong to the stable layout node, including widget leaves. */
export function LayoutSizing({document,id,onPatch}:{document:Api.PageDocument;id:string;onPatch:(id:string,patch:Partial<Api.PageLayoutNode>)=>void}) {
 const node=document.nodes[id];if(!node)return null;
 const limits=pageUIManifest.layout,update=(key:keyof Api.PageLayoutSize,value:number|string|undefined)=>{
  const size={...node.size,[key]:value};for(const key of Object.keys(size)as(keyof Api.PageLayoutSize)[])if(size[key]===undefined)delete size[key];
  onPatch(id,{size:Object.keys(size).length?size:undefined});
 };
 const dimension=(key:keyof Api.PageLayoutSize,label:string,prefix:string)=><InspectorField label={t(label)} prefix={prefix} unit="px" type="number" min={limits.minSize} max={limits.maxSize} step={1} value={node.size?.[key]??""} placeholder={t("Automatic")} onChange={e=>update(key,e.target.value===""?undefined:Number(e.target.value))}/>;
 const constrained=!!(node.size?.minWidth||node.size?.maxWidth||node.size?.minHeight||node.size?.maxHeight||node.size?.weight);
 return <div className="grid content-start"><InspectorSection title={t("Region sizing")} actions={<Button size="icon" variant="ghost" aria-label={t("Reset region sizing")} title={t("Reset region sizing")} onClick={()=>onPatch(id,{size:undefined,gap:undefined})}><RotateCcw/></Button>}>
  <div className="grid grid-cols-2 gap-2">{dimension('width','Width (px)','W')}{dimension('height','Height (px)','H')}</div>
  {["rows","columns"].includes(node.kind)&&<InspectorField label={t("Layout gap (px)")} prefix={<BetweenHorizontalStart/>} unit="px" type="number" min={0} max={limits.maxGap} step={1} value={node.gap??""} placeholder="12" onChange={e=>onPatch(id,{gap:e.target.value===""?undefined:Number(e.target.value)})}/>}
  <Select aria-label={t("Region scroll")} className="text-xs" value={node.size?.scroll??"visible"} onChange={e=>update("scroll",e.target.value==="visible"?undefined:e.target.value)}><option value="visible">{t("Natural flow")}</option><option value="auto">{t("Scroll inside region")}</option></Select>
  <Disclosure key={id} summary={<span className="text-xs">{t("Size constraints and weight")}</span>} defaultOpen={constrained}>
   <div className="grid grid-cols-2 gap-2 pt-2">{dimension('minWidth','Minimum width (px)','W≥')}{dimension('minHeight','Minimum height (px)','H≥')}{dimension('maxWidth','Maximum width (px)','W≤')}{dimension('maxHeight','Maximum height (px)','H≤')}</div>
   <div className="mt-2"><InspectorField label={t("Layout weight")} prefix="×" type="number" min={1} max={limits.maxWeight} step={1} value={node.size?.weight??""} placeholder={t("Automatic")} onChange={e=>update("weight",e.target.value===""?undefined:Number(e.target.value))}/></div>
   <p className="mt-2 text-[11px] text-muted">{t("Weights share the parent axis; row weights need a definite parent height. Narrow columns stack.")}</p>
  </Disclosure>
 </InspectorSection></div>;
}
