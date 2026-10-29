import type { Api } from "@platform/kernel";
import { useReadQuery } from "../index";

export type ProvidedCapability = Api.ProvidedCapability;
export type DependencyDescriptor = Api.Dependencies;
export type DependencyRead = Api.DependencyView;

export function useDependencies(ref?: Api.AssetRef, app?: string, enabled = true) {
  const parameters = new URLSearchParams();
  if (ref) { parameters.set("app", ref.app); parameters.set("kind", ref.kind); parameters.set("name", ref.name); }
  else if (app) parameters.set("app", app);
  return useReadQuery<DependencyRead>(`/v1/dependencies${parameters.size ? `?${parameters}` : ""}`, undefined, enabled);
}

export function useProvidedCapabilities(kind?: string, side?: string) {
  const parameters = new URLSearchParams();
  if (kind) parameters.set("kind", kind);
  if (side) parameters.set("side", side);
  return useReadQuery<ProvidedCapability[]>(`/v1/provides${parameters.size ? `?${parameters}` : ""}`);
}
