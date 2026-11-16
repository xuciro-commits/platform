import {Boxes} from "lucide-react";
import { Badge } from "@astryxdesign/core/Badge";
import { HStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import {t} from "../i18n";

export function CollectionTitle({title,value,error}:{title:string;value?:string;error?:string}){
 return <HStack gap={2} wrap="wrap" role="group" aria-label={title}>
  <Boxes aria-hidden className="size-4 shrink-0"/>
  <Text type="label" weight="semibold">{title}</Text>
  {error?<Text as="span" role="alert" className="text-danger">{error}</Text>:value===undefined?<Text type="supporting" role="status">{t("Loading…")}</Text>:<span role="status" aria-label={t("Complete collection count")}><Badge label={value}/></span>}
 </HStack>;
}
