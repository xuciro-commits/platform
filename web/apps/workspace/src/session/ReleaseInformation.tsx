import type { Api } from "@platform/kernel";
import { Button, Dialog, t } from "@platform/ui";
import type { UseQueryResult } from "@tanstack/react-query";

/** Public activation identity; candidate descriptors remain builder-only. */
export function ReleaseInformation({ query, open, onOpenChange }: { query: UseQueryResult<Api.ReleaseActive>; open: boolean; onOpenChange: (open: boolean) => void }) {
  return <Dialog open={open} onOpenChange={onOpenChange} title={t("Last activated release")}>
    <div className="grid gap-3 text-sm">
      {query.isError || query.fetchStatus === "paused" ? <p role="alert">{t("The active release could not be read. Retry to check the current identifier.")}</p> :
        query.isPending ? <p role="status">{t("Checking release…")}</p> : query.data?.id ?
          <code className="select-all break-all rounded-sm bg-background p-3 text-xs">{query.data.id}</code> : <p>{t("No activated release")}</p>}
      <p className="text-xs text-muted">{t("This identifies the last activated release. Existing workflows keep their startup release.")}</p>
      <p className="text-xs text-muted">{t("Direct installs may change workspace definitions outside this release.")}</p>
      <Button disabled={query.isFetching} onClick={() => void query.refetch()}>{query.isFetching ? t("Checking release…") : t("Refresh release")}</Button>
    </div>
  </Dialog>;
}
