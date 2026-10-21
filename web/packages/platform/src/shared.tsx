// Shared answer types and display helpers for Settings.
import type { Api } from "@platform/kernel";

// What the host answers with is generated from its Go types.
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

// The organisation: units in several structures, memberships, all dated.
export type Chart = Api.OrgSeed;
export const today = () => new Date().toISOString().slice(0, 10);
export const active = (x: { from?: string; until?: string }, day: string) => (x.from ?? "") <= day && (!x.until || day < x.until);
export const when = (at?: string) => (at ? new Date(at).toLocaleString() : "—");

export const kind = (m: Member) => (m.agent ? "AI agent" : m.subjects.some((s) => s.startsWith("client:")) ? "service" : "person");

// AI.
export type Vendor = Api.Vendor;
export type Provider = Api.Provider;
export type AIModel = Api.Model;
export type CatalogModel = Api.CatalogModel;
export type Usage = Api.Usage;
export type Total = Api.Total;
