import {useHost} from "@platform/app";
import {apiErrorMessage,type Api} from "@platform/kernel";
import {Button,Checkbox,Dialog,Input,Notice,t} from "@platform/ui";
import {useState} from "react";

export function ProductExport({candidate,assets,application}:{candidate:string;assets:Api.ReleaseAsset[];application?:string}) {
  const {client,can,decide}=useHost();
  const apps=assets.filter(asset=>asset.ref.app==="build" && asset.ref.kind==="app");
  const [open,setOpen]=useState(false),[id,setID]=useState(""),[version,setVersion]=useState("1.0.0"),[title,setTitle]=useState(""),[requires,setRequires]=useState("");
  const [selected,setSelected]=useState<string[]>([]),[busy,setBusy]=useState(false),[error,setError]=useState("");
  if(!apps.length)return null;
  const exportVersion=async(add:boolean)=>{
    setBusy(true);setError("");
    const response=await client.call<Api.PackageDescriptor>("POST",`/v1/releases/candidates/${encodeURIComponent(candidate)}/product`,{id,version,title,requires:requires.split(",").map(value=>value.trim()).filter(Boolean),roots:apps.filter(asset=>selected.includes(asset.ref.name)).map(asset=>asset.ref)});
    if(!response.ok){setError(apiErrorMessage(response.body)??t("The product version could not be created."));setBusy(false);return;}
    if(add){
      const inspected=await client.call<Api.PackagePreview>("POST","/v1/packages/inspect",{content:response.body});
      if(!inspected.ok){setError(apiErrorMessage(inspected.body)??t("The product version could not be inspected."));setBusy(false);return;}
      const imported=await decide("platform.package.import",{type:"platform.package",id},{id,digest:inspected.body.digest});
      if(!imported){setError(t("The product version could not be added to the catalog."));setBusy(false);return;}
    } else {
      const url=URL.createObjectURL(new Blob([JSON.stringify(response.body,null,2)],{type:"application/json"})),link=document.createElement("a");
      link.href=url;link.download=`${id}-${version}.json`;link.click();URL.revokeObjectURL(url);
    }
    client.refreshLiveReads();setBusy(false);setOpen(false);
  };
  return <>
    <Button onClick={()=>{setSelected([apps.find(asset=>asset.ref.name===application)?.ref.name||apps[0]!.ref.name]);setOpen(true);}}>{t("Create product version")}</Button>
    <Dialog open={open} onOpenChange={setOpen} title={t("Create product version")} wide>
      <p className="mb-3 text-sm text-muted">{t("Only the selected applications and their dependencies are exported from this saved candidate. Dependency products must already be in this catalog.")}</p>
      <div className="grid gap-3">
        <label className="grid gap-1 text-sm">{t("Product ID")}<Input value={id} onChange={event=>setID(event.target.value)}/></label>
        <label className="grid gap-1 text-sm">{t("Title")}<Input value={title} onChange={event=>setTitle(event.target.value)}/></label>
        <label className="grid gap-1 text-sm">{t("Version")}<Input value={version} placeholder="1.0.0" onChange={event=>setVersion(event.target.value)}/></label>
        <fieldset className="grid gap-2"><legend className="mb-2 text-sm font-medium">{t("Applications")}</legend>{apps.map(asset=><Checkbox key={asset.ref.name} checked={selected.includes(asset.ref.name)} onChange={checked=>setSelected(current=>checked?[...current,asset.ref.name]:current.filter(name=>name!==asset.ref.name))}>{(asset.body as {title?:string}).title||asset.ref.name}</Checkbox>)}</fieldset>
        <label className="grid gap-1 text-sm">{t("Dependency products")}<Input value={requires} placeholder="inventory@^1.0.0, identity@~2.1.0" onChange={event=>setRequires(event.target.value)}/></label>
        {error && <Notice tone="danger" role="alert" message={error}/>}
        <div className="flex flex-wrap justify-end gap-2"><Button disabled={busy} onClick={()=>setOpen(false)}>{t("Cancel")}</Button><Button disabled={busy||!id||!version||!selected.length} onClick={()=>void exportVersion(false)}>{t("Download product")}</Button>{can("platform.package.import") && <Button variant="primary" disabled={busy||!id||!version||!selected.length} onClick={()=>void exportVersion(true)}>{t("Add to Marketplace")}</Button>}</div>
      </div>
    </Dialog>
  </>;
}
