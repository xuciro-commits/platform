import type { Api } from "@platform/api";
import { Button, Dialog, t } from "@platform/ui";
import { VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import type { UseQueryResult } from "@tanstack/react-query";

export function ReleaseInformation({ query, open, onOpenChange }: { query: UseQueryResult<Api.ReleaseActive>; open: boolean; onOpenChange: (open: boolean) => void }) {
  return <Dialog open={open} onOpenChange={onOpenChange} title={t("Last activated release")}>
    <VStack gap={3}>
      {query.isError || query.fetchStatus === "paused" ? <Text as="p" role="alert">{t("The active release could not be read. Retry to check the current identifier.")}</Text> :
        query.isPending ? <Text as="p" role="status">{t("Checking release…")}</Text> : query.data?.id ?
          <Text type="code" wordBreak="break-all">{query.data.id}</Text> : <Text as="p">{t("No activated release")}</Text>}
      <Text type="supporting">{t("This identifies the last activated release. Existing workflows keep their startup release.")}</Text>
      <Text type="supporting">{t("Direct installs may change workspace definitions outside this release.")}</Text>
      <Button loading={query.isFetching} onClick={() => void query.refetch()}>{t("Refresh release")}</Button>
    </VStack>
  </Dialog>;
}