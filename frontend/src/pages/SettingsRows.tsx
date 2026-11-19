import { AspectRatio } from "@astryxdesign/core/AspectRatio";
import { Card } from "@astryxdesign/core/Card";
import { Divider } from "@astryxdesign/core/Divider";
import { Grid } from "@astryxdesign/core/Grid";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, Stack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { SelectableCard } from "@astryxdesign/core/SelectableCard";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { Theme } from "@astryxdesign/core/theme";
import { colorVars, radiusVars, spacingVars } from "@astryxdesign/core/theme/tokens.stylex";
import { ComputerDesktopIcon, MoonIcon, SunIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { Children, type ComponentType, type ReactNode, type SVGProps, useEffect, useState } from "react";
import { defaultThemeSlug, themes } from "../astryx-themes";
import { t } from "../i18n";

export type IconComponent = ComponentType<SVGProps<SVGSVGElement>>;

export const panelContainer = "settings-panel";
export const controlWidth = 192;
const stacked = `@container ${panelContainer} (max-width: 480px)`;

export const row = stylex.create({
  panel: { containerType: "inline-size", containerName: panelContainer },
  line: {
    display: "flex",
    flexDirection: { default: "row", [stacked]: "column" },
    alignItems: { default: "center", [stacked]: "stretch" },
    gap: { default: spacingVars["--spacing-3"], [stacked]: spacingVars["--spacing-2"] },
  },
  textColumn: { maxWidth: { default: 420, [stacked]: "none" } },
  control: { flexShrink: 0 },
});

const preview = stylex.create({
  bar: { height: 4, width: "50%", borderRadius: 999, backgroundColor: colorVars["--color-border-emphasized"] },
  barWide: { width: "66%" },
  clip: { overflow: "hidden", borderRadius: radiusVars["--radius-element"] },
  mutedSurface: { backgroundColor: colorVars["--color-background-muted"] },
  contentSurface: { backgroundColor: colorVars["--color-background-surface"] },
  inert: { pointerEvents: "none" },
});

export function SettingsCard({ title, children }: { title?: string; children: ReactNode }) {
  const rows = Children.toArray(children);
  return (
    <VStack gap={1.5}>
      {title && <Text type="supporting" weight="semibold" color="secondary">{title}</Text>}
      <Card padding={0} width="100%" variant="muted">
        <VStack as="ul" role="list" gap={0}>
          {rows.map((item, index) => (
            <VStack key={index} as="li" gap={0}>
              {index > 0 && <Divider variant="subtle" />}
              {item}
            </VStack>
          ))}
        </VStack>
      </Card>
    </VStack>
  );
}

export function SettingsRow({ title, titleAccessory, description, icon, control, detail }: {
  title: string; titleAccessory?: ReactNode; description?: ReactNode; icon: IconComponent; control?: ReactNode; detail?: ReactNode;
}) {
  return (
    <VStack padding={4} gap={2}>
      <Stack xstyle={row.line} data-settings-line>
        <StackItem size="fill">
          <HStack gap={2} align="start">
            <VStack paddingBlockStart={0.5}><Icon icon={icon} size="sm" color="secondary" /></VStack>
            <VStack gap={0.5} xstyle={row.textColumn}>
              <HStack gap={1.5} align="center"><Text type="label">{title}</Text>{titleAccessory}</HStack>
              {description && <Text type="supporting" color="secondary">{description}</Text>}
            </VStack>
          </HStack>
        </StackItem>
        {control && <VStack xstyle={row.control}>{control}</VStack>}
      </Stack>
      {detail}
    </VStack>
  );
}

export function TextSetting({ label, value, type, onCommit, isDisabled }: {
  label: string; value: string; type?: "text" | "email" | "tel"; onCommit: (value: string) => void; isDisabled?: boolean;
}) {
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);
  const commit = () => { if (draft.trim() !== value) onCommit(draft.trim()); };
  return (
    <TextInput label={label} isLabelHidden size="sm" width={controlWidth} type={type} value={draft} isDisabled={isDisabled}
      onChange={setDraft} onBlur={commit} onKeyDown={(event) => { if (event.key === "Enter") commit(); }} />
  );
}

function PreviewCanvas({ mode }: { mode: "light" | "dark" }) {
  return (
    <Theme theme={themes[defaultThemeSlug]} mode={mode}>
      <VStack padding={2} gap={1.5} height="100%" xstyle={preview.mutedSurface}>
        <VStack gap={1} align="center">
          <VStack xstyle={[preview.bar, preview.barWide]} />
          <VStack xstyle={preview.bar} />
        </VStack>
        <StackItem size="fill">
          <VStack padding={2} gap={1} justify="center" height="100%" xstyle={preview.contentSurface}>
            {[0, 1, 2].map((line) => <VStack key={line} xstyle={preview.bar} />)}
          </VStack>
        </StackItem>
      </VStack>
    </Theme>
  );
}

const themeChoices = [
  { value: "light", label: "Light", icon: SunIcon },
  { value: "dark", label: "Dark", icon: MoonIcon },
  { value: "system", label: "Match system", icon: ComputerDesktopIcon },
] as const;

export function ThemeChoiceCards({ value, onChange, isDisabled }: { value: string; onChange: (value: string) => void; isDisabled?: boolean }) {
  return (
    <Grid role="radiogroup" aria-label={t("Color theme")} columns={{ minWidth: 96, max: 3 }} gap={2} maxWidth={480}>
      {themeChoices.map((choice) => {
        const isSelected = choice.value === value;
        return (
          <SelectableCard key={choice.value} label={t(choice.label)} isSelected={isSelected} isDisabled={isDisabled} padding={2}
            onChange={() => { if (!isSelected) onChange(choice.value); }}>
            <VStack gap={1.5}>
              <AspectRatio ratio={3 / 2} xstyle={preview.inert}>
                <VStack aria-hidden="true" height="100%" xstyle={preview.clip}>
                  {choice.value === "system" ? (
                    <HStack gap={0} height="100%">
                      <StackItem size="fill"><PreviewCanvas mode="light" /></StackItem>
                      <StackItem size="fill"><PreviewCanvas mode="dark" /></StackItem>
                    </HStack>
                  ) : <PreviewCanvas mode={choice.value} />}
                </VStack>
              </AspectRatio>
              <HStack gap={1} align="center" justify="center">
                <Icon icon={choice.icon} size="sm" color="secondary" />
                <Text type="supporting" weight={isSelected ? "semibold" : undefined}>{t(choice.label)}</Text>
              </HStack>
            </VStack>
          </SelectableCard>
        );
      })}
    </Grid>
  );
}
