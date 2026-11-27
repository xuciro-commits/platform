import { Avatar } from "@astryxdesign/core/Avatar";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { FileInput } from "@astryxdesign/core/FileInput";
import { Grid } from "@astryxdesign/core/Grid";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Popover } from "@astryxdesign/core/Popover";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Text } from "@astryxdesign/core/Text";
import { colorVars, radiusVars } from "@astryxdesign/core/theme/tokens.stylex";
import * as stylex from "@stylexjs/stylex";
import { useMemo, useState } from "react";
import { t } from "../i18n";
import { reasonOf } from "../host";
import {
  type AvatarPackId,
  normalizeAvatarRef,
  presetAvatarUrl,
  uploadAvatarFile,
  useAvatarCatalog,
  useAvatarUrl,
} from "./avatars";

type SaveResult = Promise<unknown> | void;
type AgeFilter = "all" | "young-adult" | "middle-aged" | "senior" | "youth";

const styles = stylex.create({
  swatch: {
    borderRadius: radiusVars["--radius-full"],
    padding: 2,
    borderWidth: 2,
    borderStyle: "solid",
    borderColor: "transparent",
    cursor: "pointer",
  },
  selected: { borderColor: colorVars["--color-accent"] },
  scroller: {
    maxHeight: 240,
    overflowY: "auto",
    overscrollBehavior: "contain",
    scrollbarGutter: "stable",
  },
});

export function AvatarPicker({
  name,
  avatar,
  seed,
  isDisabled,
  onChange,
}: {
  name: string;
  avatar?: string;
  seed?: string;
  isDisabled?: boolean;
  onChange: (next: string) => SaveResult;
}) {
  const normalized = normalizeAvatarRef(avatar);
  const src = useAvatarUrl(normalized, seed || name, 256);
  const catalog = useAvatarCatalog();
  const [pack, setPack] = useState<AvatarPackId>(() => (normalized.startsWith("default/global/") ? "global" : "china"));
  const [age, setAge] = useState<AgeFilter>("all");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [open, setOpen] = useState(false);

  const visibleItems = useMemo(() => {
    return catalog.items.filter((item) => {
      if (item.pack !== pack) return false;
      if (pack !== "china" || age === "all") return true;
      if (age === "youth") return item.ageGroup === "child" || item.ageGroup === "teen";
      return item.ageGroup === age;
    });
  }, [catalog.items, pack, age]);

  const pickFile = async (file: File) => {
    if (busy || isDisabled) return;
    setBusy(true);
    setFailure("");
    try {
      const uploaded = await uploadAvatarFile(file);
      await onChange(uploaded.avatar);
    } catch (problem) {
      setFailure(reasonOf(problem) || t("Avatar could not be uploaded"));
    } finally {
      setBusy(false);
    }
  };

  const pickPreset = async (preset: string) => {
    if (busy || isDisabled) return;
    setBusy(true);
    setFailure("");
    try {
      await onChange(preset);
      setOpen(false);
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  return (
    <VStack gap={2}>
      <HStack gap={3} vAlign="center" wrap="wrap">
        <Avatar name={name} src={src} size="lg" tooltip={false} />
        <HStack gap={2} vAlign="center" wrap="wrap">
          <FileInput
            label={t("Upload image")}
            placeholder={t("Upload image")}
            accept="image/avif,image/webp,image/png,image/jpeg,image/gif"
            value={null}
            isLoading={busy}
            isDisabled={isDisabled || busy}
            onChange={(file) => { if (file instanceof File) void pickFile(file); }}
          />
          <Popover
            isOpen={open}
            onOpenChange={setOpen}
            placement="below"
            alignment="start"
            content={
              <VStack gap={2} padding={1} width={300}>
                <HStack hAlign="between" vAlign="center" gap={2}>
                  <Text type="label" color="secondary">{t("Default avatars")}</Text>
                  <SegmentedControl
                    size="sm"
                    label={t("Avatar category")}
                    value={pack}
                    onChange={(next) => {
                      setPack(next as AvatarPackId);
                      setAge("all");
                    }}
                  >
                    <SegmentedControlItem value="china" label={t("China")} />
                    <SegmentedControlItem value="global" label={t("Global")} />
                  </SegmentedControl>
                </HStack>
                {pack === "china" && (
                  <SegmentedControl
                    size="sm"
                    label={t("Age group")}
                    value={age}
                    onChange={(next) => setAge(next as AgeFilter)}
                  >
                    <SegmentedControlItem value="all" label={t("All")} />
                    <SegmentedControlItem value="young-adult" label={t("Young adult")} />
                    <SegmentedControlItem value="middle-aged" label={t("Middle-aged")} />
                    <SegmentedControlItem value="senior" label={t("Senior")} />
                    <SegmentedControlItem value="youth" label={t("Youth")} />
                  </SegmentedControl>
                )}
                <VStack xstyle={styles.scroller}>
                  <Grid columns={5} gap={2}>
                    {visibleItems.map((item) => (
                      <HStack
                        key={item.ref}
                        role="button"
                        tabIndex={0}
                        aria-label={item.name}
                        xstyle={[styles.swatch, normalized === item.ref && styles.selected]}
                        onClick={() => void pickPreset(item.ref)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter" || event.key === " ") {
                            event.preventDefault();
                            void pickPreset(item.ref);
                          }
                        }}
                      >
                        <Avatar name={item.name} src={presetAvatarUrl(item.ref, 128)} size="md" tooltip={false} />
                      </HStack>
                    ))}
                  </Grid>
                </VStack>
              </VStack>
            }
          >
            <Button label={t("Choose default")} variant="secondary" size="sm" isDisabled={isDisabled || busy} />
          </Popover>
        </HStack>
      </HStack>
      {failure && <Banner status="error" title={t("Avatar could not be uploaded")} description={failure} />}
    </VStack>
  );
}
