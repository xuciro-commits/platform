import {Panel} from "../primitives/card";
import {t} from "../i18n";
import {validExternalFrame,type ExternalFrameConfig} from "./external-frame";

export {validExternalFrame,type ExternalFrameConfig} from "./external-frame";
export type ExternalFrameProps={config:ExternalFrameConfig;title:string;active?:boolean};
/** All sandbox restrictions stay enabled; no platform message or token bridge. */
export function ExternalFrame({config,title,active=true}:ExternalFrameProps) {
 if(!validExternalFrame(config)||typeof window!=="undefined"&&config.origin===window.location.origin)return <Panel role="alert">{t("The external document origin or URL is unavailable.")}</Panel>;
 if(!active)return null;
 return <div className="min-w-0 grid gap-1">
  <iframe title={title} src={config.url} height={config.height??240} className="w-full border-0" sandbox="" referrerPolicy="no-referrer" loading="lazy" allow="camera 'none'; microphone 'none'; geolocation 'none'; clipboard-read 'none'; clipboard-write 'none'; fullscreen 'none'; payment 'none'"/>
  <p className="text-xs text-muted">{t("Isolated external document. Scripts, forms and platform data access are disabled.")}</p>
 </div>;
}
