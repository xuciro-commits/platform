import { useToast } from "@astryxdesign/core/Toast";
import { type Api, useRead } from "../host";
import { reasonOf, send } from "../pages/act";

export type ProfileValues = Partial<Record<keyof Api.Account, unknown>>;

export function useAccount() {
  const toast = useToast();
  const me = useRead<Api.MeView>("/v1/me");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const data = me.status === "ready" ? me.data : undefined;
  const update = catalog.status === "ready" ? catalog.data.find((action) => action.schema === "platform.profile.update") : undefined;
  const save = (values: ProfileValues) => {
    if (!update || !data) return;
    send(update, data.principalId, values).catch((problem: unknown) => toast({ body: reasonOf(problem), type: "error" }));
  };
  return { me: data, account: data?.account, name: data?.account.displayName || data?.principalId || "", canSave: Boolean(update && data), save };
}
