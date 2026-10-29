import type { Api } from "@platform/kernel";
import { formatStamp, t } from "@platform/ui";

export type Member = Api.MemberView;
export type AppInfo = Api.AppInfo;
export type ProtocolInfo = Api.ProtocolInfo;
export type Delivery = Api.Delivery;
export type Task = Api.Task;
export type Connector = Api.ConnectorView;
export type EndpointView = Api.EndpointView;
export type Effect = Api.Effect;
export type SettingValue = Api.SettingValue;
export type AppSettings = Api.AppSettings;
export type AuditEntry = Api.AuditEntry;

export type Chart = Api.OrgSeed;
export const today = () => new Date().toISOString().slice(0, 10);
export const active = (x: { from?: string; until?: string }, day: string) => (x.from ?? "") <= day && (!x.until || day < x.until);
export const when = (at?: string) => formatStamp(at);

export const kind = (m: Member) => (m.agent ? "AI agent" : m.subjects.some((s) => s.startsWith("client:")) ? "service" : "person");

export type Vendor = Api.Vendor;
export type Provider = Api.Provider;
export type AIModel = Api.Model;
export type CatalogModel = Api.CatalogModel;
export type Usage = Api.Usage;
export type Total = Api.Total;

export function modelReferenceTitle(reference:string):string {
  const labels:Record<string,string>={"ai/app-model":"Application generation","agent/model":"Agent tasks","knowledge/embedding-model":"Knowledge search"};
  return labels[reference]?t(labels[reference]):reference;
}
