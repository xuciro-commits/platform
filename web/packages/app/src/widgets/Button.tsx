import { Button, Reasoned, t, useActionAvailability } from "@platform/ui";
import { useHost } from "../index";

export type ButtonPorts = { title?: string; enabled?: boolean; onClick?: () => void; actionGate?: { schema: string; record: { id: string; revision: number } } };
export function ButtonRenderer({title,enabled,onClick,actionGate}:ButtonPorts) {
  const {source,action}=useHost(),target=actionGate?action(actionGate.schema)?.target:undefined;
  const transition=!!target&&!!source.entity(target)?.lifecycle?.transitions.some(step=>step.schema===actionGate!.schema);
  const verdict=useActionAvailability(source,transition&&actionGate?{type:target!,id:actionGate.record.id,revision:actionGate.record.revision}:undefined,transition?[actionGate!.schema]:[])[actionGate?.schema??""];
  const blocked=verdict?.allowed===false;
  return <Reasoned reason={blocked?verdict.reason||t("This action is not available for this record."):undefined}><Button onClick={onClick} disabled={!onClick||enabled===false||blocked}>{title||t("Button")}</Button></Reasoned>;
}
