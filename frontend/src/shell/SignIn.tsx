import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Center } from "@astryxdesign/core/Center";
import { Icon } from "@astryxdesign/core/Icon";
import { VStack } from "@astryxdesign/core/Layout";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Heading, Text } from "@astryxdesign/core/Text";
import { CubeIcon } from "@heroicons/react/24/outline";
import { type Api, startSession, useRead } from "../host";
import { t } from "../i18n";

export function SignIn() {
  const options = useRead<Api.SignIn>("/v1/sign-in");
  return (
    <Center axis="both" padding={6} minHeight="100dvh">
      <VStack gap={4} hAlign="center" width="100%" maxWidth={400}>
        <VStack gap={2} hAlign="center">
          <Icon icon={CubeIcon} size="lg" />
          <Text type="body" weight="bold" size="lg">
            {t("Platform")}
          </Text>
        </VStack>
        <Card padding={8} width="100%">
          <VStack gap={4} hAlign="stretch">
            <VStack gap={1} hAlign="center">
              <Heading level={1}>{t("Sign in")}</Heading>
              <Text type="body" color="secondary" size="sm">
                {t("Choose how to continue")}
              </Text>
            </VStack>
            {options.status === "loading" && <Skeleton height={44} />}
            {(options.status === "error" || options.status === "refused") && (
              <Banner status="error" title={t("The platform could not be reached")} description={options.reason} />
            )}
            {options.status === "ready" && (
              <VStack gap={3} hAlign="stretch">
                {(options.data.identities ?? []).map((identity) => (
                  <Button
                    key={`${identity.tenant}/${identity.token}`}
                    label={`${identity.member} · ${identity.tenant}`}
                    variant="secondary"
                    size="lg"
                    onClick={() => startSession({ token: identity.token, tenant: identity.tenant })}
                  />
                ))}
                {options.data.issuer && <Banner status="info" title={t("Single sign-on is not connected to this frontend yet")} />}
              </VStack>
            )}
          </VStack>
        </Card>
      </VStack>
    </Center>
  );
}
