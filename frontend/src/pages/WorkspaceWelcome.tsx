import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Center } from "@astryxdesign/core/Center";
import { VStack } from "@astryxdesign/core/Layout";
import { Heading, Text } from "@astryxdesign/core/Text";
import * as stylex from "@stylexjs/stylex";
import { t } from "../i18n";
import { navigate } from "../router";

const styles = stylex.create({
  artwork: { width: "min(100%, 560px)", aspectRatio: "20 / 11", backgroundImage: "url(/media/workspace-empty.avif)", backgroundPosition: "center", backgroundSize: "contain", backgroundRepeat: "no-repeat" },
});

export function WorkspaceWelcome() {
  return (
    <Center axis="both" padding={6} minHeight="70dvh">
      <Card padding={8} maxWidth={760} width="100%">
        <VStack gap={4} hAlign="center">
          <Card variant="transparent" padding={0} xstyle={styles.artwork} />
          <Heading level={2}>{t("Your workspace is ready")}</Heading>
          <Text type="body" color="secondary">{t("Install your first application to bring your workspace to life.")}</Text>
          <Button variant="primary" size="lg" label={t("Explore marketplace")} onClick={() => void navigate("/delivery/marketplace")} />
        </VStack>
      </Card>
    </Center>
  );
}
