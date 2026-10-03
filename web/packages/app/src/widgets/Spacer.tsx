import {Spacer,Panel,t} from "@platform/ui";
import {pageUIManifest,type Api} from "@platform/kernel";
export function SpacerRenderer({config}:{config?:Api.PageSpacer}){
 const size=config?.size;
 if(typeof size!=="number"||!Number.isFinite(size)||size<0||size>pageUIManifest.layout.maxSize)return <Panel role="alert">{t("Spacer configuration is unavailable or incompatible.")}</Panel>;
 return <Spacer size={size}/>;
}
