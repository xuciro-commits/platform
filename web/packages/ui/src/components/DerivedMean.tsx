import { VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import {t} from "../i18n";
export function DerivedMean({count,mean,field,unit}:{count:string;mean?:number;field:string;unit:string}) {
 if(!/^(0|[1-9][0-9]*)$/.test(count)||!field||typeof unit!=="string"||mean!==undefined&&!Number.isFinite(mean))return <Text as="p" role="alert">{t("Original derived mean values are unavailable or incompatible.")}</Text>;
 return <VStack gap={2}><Text type="body">{t("Original source set: {count} records",{count})}</Text><Text type="body">{t("Expression")}: <Text as="span" type="code">mean({field})</Text></Text><Text type="body" size="2xl" weight="semibold" hasTabularNumbers>{mean===undefined?t("No numeric values"):mean.toFixed(2)}{unit&&` ${unit}`}</Text><Text type="supporting">{t("The complete set count includes missing values; the average uses the original host's numeric-value semantics.")}</Text></VStack>;
}
