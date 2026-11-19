import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, Layout, LayoutContent, LayoutPanel, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { Selector } from "@astryxdesign/core/Selector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Switch } from "@astryxdesign/core/Switch";
import { Heading } from "@astryxdesign/core/Text";
import { useToast } from "@astryxdesign/core/Toast";
import { Toolbar } from "@astryxdesign/core/Toolbar";
import { ArrowLeftIcon, ChevronRightIcon, Cog6ToothIcon, HashtagIcon, ListBulletIcon, PencilSquareIcon, PowerIcon } from "@heroicons/react/24/outline";
import { useState } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import { PageTitle, useGutter } from "./frame";
import { controlWidth, type IconComponent, row, SettingsCard, SettingsRow, TextSetting } from "./SettingsRows";

export type SettingItem = { name: string; title: string; description?: string; type: string; value: string; choices?: string[] };
export type SettingsGroup = { id: string; label: string; settings: SettingItem[] };
export type SettingsDefinition<Answer> = {
  title: string;
  about: string;
  read: string;
  groups: (answer: Answer) => SettingsGroup[];
  action: string;
  target: (group: string, setting: string) => string;
};

const icons: Record<string, IconComponent> = { boolean: PowerIcon, choice: ListBulletIcon, integer: HashtagIcon };

function Control({ setting, onCommit }: { setting: SettingItem; onCommit: (value: string) => void }) {
  if (setting.type === "boolean") return <Switch label={setting.title} isLabelHidden value={setting.value === "true"} onChange={(on) => onCommit(String(on))} />;
  if (setting.choices?.length) {
    return <Selector label={setting.title} isLabelHidden size="sm" width={controlWidth} value={setting.value} options={setting.choices.map((choice) => ({ value: choice, label: choice }))} onChange={onCommit} />;
  }
  return <TextSetting label={setting.title} value={setting.value} onCommit={onCommit} />;
}

export function settingsPage<Answer>(definition: SettingsDefinition<Answer>) {
  return function SettingsEntry() {
    const read = useRead<Answer>(definition.read);
    const catalog = useRead<Api.Action[]>("/v1/actions");
    const toast = useToast();
    const narrow = useMediaQuery("(max-width: 768px)");
    const gutter = useGutter();
    const [chosen, setChosen] = useState("");
    const [detail, setDetail] = useState(false);
    const groups = read.status === "ready" ? definition.groups(read.data) : [];
    const group = groups.find((candidate) => candidate.id === chosen) ?? groups[0];
    const action = catalog.status === "ready" ? catalog.data.find((candidate) => candidate.schema === definition.action) : undefined;
    const commit = (setting: string) => async (value: string) => {
      if (!action || !group) return;
      try {
        await send(action, definition.target(group.id, setting), { value });
      } catch (problem) {
        toast({ body: reasonOf(problem), type: "error" });
      }
    };
    const choose = (id: string) => {
      setChosen(id);
      setDetail(true);
    };

    const nav = (
      <VStack gap={4} padding={gutter}>
        <PageTitle title={t(definition.title)} />
        <List density="spacious" aria-label={t("Apps")}>
          {groups.map((candidate) => (
            <ListItem
              key={candidate.id}
              label={t(candidate.label)}
              startContent={<Icon icon={Cog6ToothIcon} />}
              endContent={narrow ? <Icon icon={ChevronRightIcon} size="sm" color="secondary" /> : undefined}
              isSelected={!narrow && candidate.id === group?.id}
              onClick={() => choose(candidate.id)}
            />
          ))}
        </List>
      </VStack>
    );
    const body = group && (
      <VStack gap={4} xstyle={row.panel}>
        {narrow ? (
          <Toolbar
            label={t(group.label)}
            gap={2}
            startContent={
              <>
                <Button label={t("Back")} variant="ghost" size="sm" isIconOnly icon={<Icon icon={ArrowLeftIcon} size="sm" />} onClick={() => setDetail(false)} />
                <Heading level={2}>{t(group.label)}</Heading>
              </>
            }
          />
        ) : (
          <HStack minHeight={36} vAlign="center">
            <Heading level={2}>{t(group.label)}</Heading>
          </HStack>
        )}
        <SettingsCard>
          {group.settings.map((setting) => (
            <SettingsRow
              key={`${group.id}/${setting.name}`}
              title={setting.title}
              description={setting.description || undefined}
              icon={icons[setting.type] ?? PencilSquareIcon}
              control={<Control setting={setting} onCommit={commit(setting.name)} />}
            />
          ))}
        </SettingsCard>
      </VStack>
    );

    if (read.status !== "ready") {
      return (
        <Layout height="fill" content={
          <LayoutContent role="main" padding={gutter} label={t(definition.title)}>
            <VStack gap={4}>
              <PageTitle title={t(definition.title)} />
              {read.status === "loading" && [0, 1, 2, 3].map((index) => <Skeleton key={index} height={48} index={index} />)}
              {read.status === "error" && <Banner status="error" title={t("This list could not be loaded")} description={read.reason || t("Reload the page to try again.")} />}
              {read.status === "refused" && <EmptyState headingLevel={2} title={t("You cannot see this list")} description={read.reason || t("Ask an administrator of this workspace for access.")} />}
            </VStack>
          </LayoutContent>
        } />
      );
    }
    if (!group) {
      return (
        <Layout height="fill" content={
          <LayoutContent role="main" padding={gutter} label={t(definition.title)}>
            <VStack gap={4}>
              <PageTitle title={t(definition.title)} />
              <EmptyState headingLevel={2} title={t("Nothing here yet")} description={t(definition.about)} />
            </VStack>
          </LayoutContent>
        } />
      );
    }
    if (narrow && !detail) {
      return <Layout height="fill" content={<LayoutContent role="main" padding={0} label={t(definition.title)}>{nav}</LayoutContent>} />;
    }
    return (
      <Layout
        height="fill"
        contentWidth={1200}
        start={narrow ? undefined : <LayoutPanel hasDivider padding={0}>{nav}</LayoutPanel>}
        content={
          <LayoutContent role="main" padding={gutter} label={t(definition.title)}>
            {body}
          </LayoutContent>
        }
      />
    );
  };
}
