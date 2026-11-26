import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Center } from "@astryxdesign/core/Center";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Heading, Text } from "@astryxdesign/core/Text";
import * as stylex from "@stylexjs/stylex";
import { t } from "../i18n";
import { navigate } from "../router";
import { useIsDark } from "../shell/appearance";
import { AnatomyPlate } from "./rebac/RebacVisuals";
import { RequestAccessButton } from "./rebac/RequestAccessDialog";

const styles = stylex.create({
  artwork: { width: "min(100%, 560px)", aspectRatio: "20 / 11", backgroundImage: "url(/media/workspace-empty.avif)", backgroundPosition: "center", backgroundSize: "contain", backgroundRepeat: "no-repeat" },
});

export function WorkspaceWelcome({ canInstall = true }: { canInstall?: boolean }) {
  const dark = useIsDark();
  return (
    <Center axis="both" padding={6} minHeight="70dvh">
      <Card padding={8} maxWidth={760} width="100%">
        <VStack gap={4} hAlign="center">
          {canInstall ? (
            <Card variant="transparent" padding={0} xstyle={styles.artwork} />
          ) : (
            <AnatomyPlate kind="gate" dark={dark} />
          )}
          <Heading level={2}>{t("Your workspace is ready")}</Heading>
          <Text type="body" color="secondary">
            {canInstall
              ? t("Install your first application to bring your workspace to life.")
              : t("Request access to an application or project to start working in this workspace.")}
          </Text>
          <HStack gap={2} wrap="wrap" hAlign="center">
            {canInstall ? (
              <Button variant="primary" size="lg" label={t("Explore marketplace")} onClick={() => void navigate("/delivery/marketplace")} />
            ) : (
              <RequestAccessButton variant="primary" size="lg" />
            )}
          </HStack>
        </VStack>
      </Card>
    </Center>
  );
}
