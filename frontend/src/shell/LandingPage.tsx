import { AspectRatio } from "@astryxdesign/core/AspectRatio";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Heading } from "@astryxdesign/core/Heading";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, Layout, LayoutContent, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Section } from "@astryxdesign/core/Section";
import { Text } from "@astryxdesign/core/Text";
import { colorVars, radiusVars } from "@astryxdesign/core/theme/tokens.stylex";
import { ArrowRightIcon, CubeIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { useSession } from "../host";
import { t } from "../i18n";

const styles = stylex.create({
  shell: {
    maxWidth: 1120,
    width: "100%",
    marginInline: "auto",
  },
  heroCopy: {
    maxWidth: 680,
  },
  heroFrame: {
    maxWidth: 1120,
    width: "100%",
    marginInline: "auto",
    overflow: "hidden",
    borderRadius: radiusVars["--radius-page"],
    borderWidth: 1,
    borderStyle: "solid",
    borderColor: colorVars["--color-border"],
  },
  heroVisual: {
    width: "100%",
    height: "100%",
    backgroundImage: "url(/media/sign-in.avif)",
    backgroundSize: "cover",
    backgroundPosition: "center",
  },
});

export function LandingPage() {
  const session = useSession();
  return (
    <Layout
      content={
        <LayoutContent padding={6}>
          <VStack gap={10} xstyle={styles.shell}>
            <HStack gap={3} vAlign="center" width="100%">
              <HStack gap={2} vAlign="center">
                <Icon icon={CubeIcon} />
                <Text type="body" weight="bold">{t("Platform")}</Text>
              </HStack>
              <StackItem size="fill" />
              {session ? (
                <Button label={t("Open workspace")} variant="secondary" size="sm" href="/" />
              ) : (
                <HStack gap={3} vAlign="center">
                  <Link href="/sign-in" isStandalone>{t("Sign in")}</Link>
                  <Button label={t("Get started")} variant="primary" size="sm" href="/signup" />
                </HStack>
              )}
            </HStack>
            <VStack gap={6} hAlign="center">
              <VStack gap={3} hAlign="center" xstyle={styles.heroCopy}>
                <Heading level={1} type="display-2" justify="center" textWrap="balance">
                  {t("Declare business objects, get a working workspace")}
                </Heading>
                <Text type="body" color="secondary" justify="center" textWrap="balance">
                  {t("Model records, actions and roles once. Lists, boards, record pages and audit trails are generated from types, and declarative packages install in minutes.")}
                </Text>
              </VStack>
              <HStack gap={3}>
                <Button
                  label={t("Get started")}
                  variant="primary"
                  size="lg"
                  href="/signup"
                  endContent={<Icon icon={ArrowRightIcon} size="sm" color="inherit" />}
                />
                <Button label={t("Sign in")} variant="secondary" size="lg" href="/sign-in" />
              </HStack>
            </VStack>
            <Section variant="transparent" padding={0}>
              <AspectRatio ratio={16 / 9} xstyle={styles.heroFrame}>
                <Card variant="transparent" padding={0} xstyle={styles.heroVisual} />
              </AspectRatio>
            </Section>
          </VStack>
        </LayoutContent>
      }
    />
  );
}
