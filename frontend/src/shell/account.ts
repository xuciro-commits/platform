import { useToast } from "@astryxdesign/core/Toast";
import { useEffect } from "react";
import { type Api, rememberAccount, useRead, reasonOf, send } from "../host";

export type ProfileValues = Partial<Record<keyof Api.Account, unknown>>;

export function useAccount() {
  const toast = useToast();
  const me = useRead<Api.MeView>("/v1/me");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const data = me.status === "ready" ? me.data : undefined;
  const name = data?.account.displayName || data?.principalId || "";
  useEffect(() => {
    if (!data) return;
    rememberAccount({
      id: data.principalId,
      name,
      email: data.account.effective.email || data.account.email || undefined,
      tenantName: data.tenant.name || data.tenantId,
    });
  }, [data, name]);
  const update = catalog.status === "ready" ? catalog.data.find((action) => action.schema === "platform.profile.update") : undefined;
  const save = (values: ProfileValues) => {
    if (!update || !data) return;
    send(update, data.principalId, values).catch((problem: unknown) => toast({ body: reasonOf(problem), type: "error" }));
  };
  return { me: data, account: data?.account, name, canSave: Boolean(update && data), save };
}
