import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { HStack, Layout, LayoutContent, LayoutFooter, LayoutHeader, LayoutPanel, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Step, Stepper } from "@astryxdesign/core/Stepper";
import { Heading, Text } from "@astryxdesign/core/Text";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import * as stylex from "@stylexjs/stylex";
import type { ReactNode } from "react";
import { t } from "../i18n";

export type WizardStep = {
  id: string;
  label: string;
  description: string;
  help: string[];
  hasError?: boolean;
};

type FormWizardProps = {
  title: string;
  steps: WizardStep[];
  activeStep: number;
  onStepClick?: (index: number) => void;
  onBack?: () => void;
  onNext: () => void;
  onClose: () => void;
  primaryLabel?: string;
  canProceed?: boolean;
  busy?: boolean;
  error?: string;
  children: ReactNode;
};

const styles = stylex.create({
  shell: { height: "min(680px, calc(100dvh - 4rem))" },
});

export function FormWizard({
  title,
  steps,
  activeStep,
  onStepClick,
  onBack,
  onNext,
  onClose,
  primaryLabel,
  canProceed = true,
  busy = false,
  error,
  children,
}: FormWizardProps) {
  const showSideRail = useMediaQuery("(min-width: 820px)");
  const showGuidance = useMediaQuery("(min-width: 1080px)");
  const current = steps[activeStep];
  const isLast = activeStep >= steps.length - 1;

  const stepper = (orientation: "vertical" | "horizontal") => (
    <Stepper
      activeStep={activeStep}
      orientation={orientation}
      indicatorPosition="on-track"
      label={title}
      horizontalOptions={{ minimumStepWidth: 112, collapsedVariant: "withLabel" }}
      onStepClick={onStepClick ? (index) => { if (index < activeStep && !busy) onStepClick(index); } : undefined}
    >
      {steps.map((step, index) => (
        <Step
          key={step.id}
          step={index}
          label={step.label}
          description={orientation === "vertical" ? step.description : undefined}
          status={step.hasError ? "error" : undefined}
          isDisabled={index > activeStep}
        />
      ))}
    </Stepper>
  );

  return (
    <Dialog isOpen onOpenChange={() => !busy && onClose()} width={showGuidance ? 1040 : 840} padding={0} purpose="form">
      <VStack xstyle={styles.shell}>
        <Layout
          height="fill"
          padding={0}
          header={
            <LayoutHeader padding={4} hasDivider>
              <DialogHeader title={title} onOpenChange={() => !busy && onClose()} />
            </LayoutHeader>
          }
          start={showSideRail ? (
            <LayoutPanel width={240} padding={4} isScrollable hasDivider>
              {stepper("vertical")}
            </LayoutPanel>
          ) : undefined}
          end={showGuidance && current && current.help.length > 0 ? (
            <LayoutPanel width={256} padding={4} role="complementary" isScrollable hasDivider>
              <VStack gap={3}>
                <Heading level={4}>{t("Guidance")}</Heading>
                {current.help.map((item, index) => (
                  <Text key={index} type="supporting" color="secondary">{item}</Text>
                ))}
              </VStack>
            </LayoutPanel>
          ) : undefined}
          content={
            <LayoutContent isScrollable padding={5}>
              <VStack gap={4}>
                {!showSideRail && stepper("horizontal")}
                {current && (
                  <VStack gap={1}>
                    <Heading level={3}>{current.label}</Heading>
                    <Text type="supporting" color="secondary">{current.description}</Text>
                  </VStack>
                )}
                {error && <Banner status="error" title={t("Not done")} description={error} />}
                {children}
              </VStack>
            </LayoutContent>
          }
          footer={
            <LayoutFooter padding={4} hasDivider>
              <HStack gap={2} vAlign="center">
                <Button label={t("Cancel")} variant="secondary" isDisabled={busy} onClick={onClose} />
                <StackItem size="fill" />
                {activeStep > 0 && onBack && (
                  <Button label={t("Back")} variant="secondary" isDisabled={busy} onClick={onBack} />
                )}
                <Button
                  label={primaryLabel ?? (isLast ? t("Done") : t("Continue"))}
                  variant="primary"
                  isDisabled={busy || !canProceed}
                  onClick={onNext}
                />
              </HStack>
            </LayoutFooter>
          }
        />
      </VStack>
    </Dialog>
  );
}
