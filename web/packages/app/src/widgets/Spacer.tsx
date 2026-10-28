import {pageUIManifest} from "../pageui/manifest";
import type * as PageUI from "../pageui/types";
import {Spacer,Panel,t} from "@platform/ui";
export function SpacerRenderer({config}:{config?:PageUI.PageSpacer}){
 const size=config?.size;
 if(typeof size!=="number"||!Number.isFinite(size)||size<0||size>pageUIManifest.layout.maxSize)return <Panel role="alert">{t("Spacer configuration is unavailable or incompatible.")}</Panel>;
 return <Spacer size={size}/>;
}
