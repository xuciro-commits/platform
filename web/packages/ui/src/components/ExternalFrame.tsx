import {Panel} from "../primitives/card";
import {VStack} from "@astryxdesign/core/Stack";
import {Text} from "@astryxdesign/core/Text";
import {t} from "../i18n";
import {validExternalFrame,type ExternalFrameConfig} from "./external-frame";

export {validExternalFrame,type ExternalFrameConfig} from "./external-frame";
export type ExternalFrameProps={config:ExternalFrameConfig;title:string;active?:boolean};
export function ExternalFrame({config,title,active=true}:ExternalFrameProps) {
 if(!validExternalFrame(config)||typeof window!=="undefined"&&config.origin===window.location.origin)return <Panel role="alert">{t("The external document origin or URL is unavailable.")}</Panel>;
 if(!active)return null;
 return <VStack gap={1}><iframe title={title} src={config.url} height={config.height??240} className="w-full border-0" sandbox="" referrerPolicy="no-referrer" loading="lazy" allow="camera 'none'; microphone 'none'; geolocation 'none'; clipboard-read 'none'; clipboard-write 'none'; fullscreen 'none'; payment 'none'"/><Text type="supporting">{t("Isolated external document. Scripts, forms and platform data access are disabled.")}</Text></VStack>;
}
