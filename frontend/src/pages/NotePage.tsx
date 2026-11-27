import { Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { Markdown } from "@astryxdesign/core/Markdown";
import { Text } from "@astryxdesign/core/Text";
import { t } from "../i18n";
import { PageTitle, useGutter } from "./frame";

export function NotePage({ title, body }: { title: string; body: string }) {
  const gutter = useGutter();
  return (
    <Layout
      height="fill"
      contentWidth={960}
      header={
        <LayoutHeader padding={0} hasDivider>
          <VStack padding={gutter} paddingBlockStart={3} paddingBlockEnd={3}>
            <PageTitle title={t(title)} />
          </VStack>
        </LayoutHeader>
      }
      content={
        <LayoutContent padding={gutter} role="main">
          <VStack gap={4}>
            {body ? <Markdown>{body}</Markdown> : <Text type="supporting" color="secondary">{t("No content yet")}</Text>}
          </VStack>
        </LayoutContent>
      }
    />
  );
}
