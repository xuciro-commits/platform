import { Pivot, Panel, t, type AggregateQuery } from "@platform/ui";

export type PivotPorts = {
  object:string; query:Omit<AggregateQuery,"groups"|"measures">; rows:string; columns?:string; measure:string;
  source?:Parameters<typeof Pivot>[0]["source"];
};
export function PivotRenderer({object,query,rows,columns,measure,source}:PivotPorts) {
  if(!rows)return <Panel role="status">{t("Choose a row grouping before saving.")}</Panel>;
  if(!source)return <Panel role="alert">{t("No source for records")}</Panel>;
  return <Pivot source={source} type={object} query={query} rows={rows} columns={columns} measure={measure}/>;
}
