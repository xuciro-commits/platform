// What every area of Settings shares: the host's answer types and the administrator's host.
import { useHost, useReadQuery as useRead } from "@platform/app";
import type { EdgeClient, Api } from "@platform/kernel";

// What the host answers with is generated from its Go types (ADR-0023 D7).
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

export type Admin = {
  client: EdgeClient; apps: AppInfo[];
  decide: (schema: string, member: string, payload: unknown) => Promise<boolean>;
  decideOn: (schema: string, target: { type: string; id: string }, payload: unknown) => Promise<boolean>;
};

// The organisation (ADR-0012): units in several structures, memberships, all dated.
export type Edge = Api.Edge;
export type Chart = Api.OrgSeed;
export const today = () => new Date().toISOString().slice(0, 10);
export const active = (x: { from?: string; until?: string }, day: string) => (x.from ?? "") <= day && (!x.until || day < x.until);
// The administrator's host: decisions about members, and about any target.
export function useAdmin(): Admin {
  const host = useHost();
  const apps = useRead<AppInfo[]>("/v1/apps").data ?? [];
  return { client: host.client, apps,
    decide: (schema, member, payload) => host.decide(schema, { type: "platform.member", id: member }, payload),
    decideOn: (schema, target, payload) => host.decide(schema, target, payload) };
}
export const when = (at?: string) => (at ? new Date(at).toLocaleString() : "—");

export const kind = (m: Member) => (m.agent ? "AI agent" : m.subjects.some((s) => s.startsWith("client:")) ? "service" : "person");

// AI (ADR-0015).
export type Vendor = Api.Vendor;
export type Provider = Api.Provider;
export type AIModel = Api.Model;
export type CatalogModel = Api.CatalogModel;
export type Usage = Api.Usage;
export type Total = Api.Total;
