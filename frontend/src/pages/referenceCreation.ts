import { type ComponentType, createContext, useContext } from "react";
import type { Api } from "../host";

export type ReferenceCreationProps = {
  action: Api.Action;
  described: Api.EntityInfo;
  onClose: () => void;
  onDone: (id: string) => void;
};

export const ReferenceCreation = createContext<ComponentType<ReferenceCreationProps> | null>(null);

export function useReferenceCreation() {
  const creator = useContext(ReferenceCreation);
  if (!creator) throw new Error("Reference creation must be provided by the application root");
  return creator;
}
