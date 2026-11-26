import { Button } from "@astryxdesign/core/Button";
import { Dialog } from "@astryxdesign/core/Dialog";
import { Divider } from "@astryxdesign/core/Divider";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, StackItem, VStack } from "@astryxdesign/core/Layout";
import { SideNav, SideNavItem, SideNavSection } from "@astryxdesign/core/SideNav";
import { Heading, Text } from "@astryxdesign/core/Text";
import { colorVars, radiusVars, spacingVars } from "@astryxdesign/core/theme/tokens.stylex";
import { BellIcon, GlobeAltIcon, KeyIcon, LockClosedIcon, PaintBrushIcon, UserCircleIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { useId, useState } from "react";
import { t } from "../../i18n";
import { useAccount } from "../account";
import { AppearancePanel, LanguagePanel, NotificationsPanel, ProfilePanel, SecurityPanel } from "./panels";
import { SelfAccessPanel } from "../../pages/rebac/SelfAccessPanel";
import { type IconComponent, row } from "../../pages/SettingsRows";

type PanelId = "profile" | "access" | "security" | "appearance" | "notifications" | "language";
type Panel = { id: PanelId; label: string; description: string; icon: IconComponent };

const groups: { label: string; panels: Panel[] }[] = [
  { label: "Account", panels: [
    { id: "profile", label: "Profile", description: "How you appear to everyone else in the workspace.", icon: UserCircleIcon },
    { id: "access", label: "Access & delegation", description: "Your active roles, access requests and temporary colleague delegations.", icon: KeyIcon },
    { id: "security", label: "Login & security", description: "The devices signed in as you right now.", icon: LockClosedIcon },
  ] },
  { label: "Preferences", panels: [
    { id: "appearance", label: "Appearance", description: "How the workspace looks for you.", icon: PaintBrushIcon },
    { id: "notifications", label: "Notifications", description: "What reaches you, and where it arrives.", icon: BellIcon },
    { id: "language", label: "Language & region", description: "Language, formats and time zone.", icon: GlobeAltIcon },
  ] },
];
const panels = groups.flatMap((group) => group.panels);

const styles = stylex.create({
  sideNav: { flexShrink: 0 },
  sideNavSection: { paddingBlock: spacingVars["--spacing-2"] },
  closeAnchor: { position: "sticky", insetBlockStart: 0, zIndex: 1, height: 0 },
  closeBacking: { display: "flex", backgroundColor: colorVars["--color-background-surface"], borderRadius: radiusVars["--radius-element"] },
  shell: { height: "min(800px, calc(100dvh - 2rem))" },
});

function PanelBody({ panel, account }: { panel: PanelId; account: ReturnType<typeof useAccount> }) {
  if (panel === "profile") return <ProfilePanel account={account} />;
  if (panel === "access") return <SelfAccessPanel />;
  if (panel === "security") return <SecurityPanel />;
  if (panel === "appearance") return <AppearancePanel account={account} />;
  if (panel === "notifications") return <NotificationsPanel account={account} />;
  return <LanguagePanel account={account} />;
}

function PanelHeading({ panel }: { panel: Panel }) {
  return (
    <VStack gap={0.5}>
      <Heading level={2}>{t(panel.label)}</Heading>
      <Text type="supporting" color="secondary">{t(panel.description)}</Text>
    </VStack>
  );
}

function Close({ onClose }: { onClose: () => void }) {
  return <IconButton label={t("Close")} variant="ghost" size="sm" icon={<Icon icon="close" size="sm" />} onClick={onClose} />;
}

export type SettingsPanelId = PanelId;

export function SettingsDialog({ onClose, onPanel, initialPanel = "profile" }: { onClose: () => void; onPanel?: (panel: PanelId) => void; initialPanel?: PanelId }) {
  const [active, choose] = useState<PanelId>(initialPanel);
  const setActive = (next: PanelId) => {
    choose(next);
    onPanel?.(next);
  };
  const titleId = useId();
  const isNarrow = useMediaQuery("(max-width: 640px)");
  const account = useAccount();
  const panel = panels.find((candidate) => candidate.id === active) ?? panels[0];
  const body = (
    <VStack gap={4} xstyle={row.panel}>
      <HStack gap={2} align="start">
        <StackItem size="fill"><PanelHeading panel={panel} /></StackItem>
        {!isNarrow && <VStack aria-hidden="true" width={32} />}
      </HStack>
      <PanelBody panel={active} account={account} />
    </VStack>
  );
  return (
    <Dialog isOpen onOpenChange={(open) => { if (!open) onClose(); }} purpose="form" width={1120}
      maxHeight="min(800px, calc(100dvh - 2rem))" padding={0} aria-labelledby={titleId}>
      <VStack xstyle={styles.shell}>
        {isNarrow ? (
          <VStack gap={0} height="100%">
            <HStack gap={2} align="center" paddingInline={3} paddingBlock={3}>
              <StackItem size="fill"><Text type="label" id={titleId}>{t("Settings")}</Text></StackItem>
              <Close onClose={onClose} />
            </HStack>
            <Divider />
            <HStack as="nav" aria-label={t("Settings sections")} gap={1} wrap="nowrap" isScrollable paddingInline={3} paddingBlock={2}>
              {panels.map((candidate) => (
                <Button key={candidate.id} label={t(candidate.label)} icon={<Icon icon={candidate.icon} size="sm" />}
                  variant={candidate.id === active ? "secondary" : "ghost"} size="sm" onClick={() => setActive(candidate.id)} />
              ))}
            </HStack>
            <Divider />
            <StackItem size="fill"><VStack padding={4} isScrollable height="100%">{body}</VStack></StackItem>
          </VStack>
        ) : (
          <Layout
            start={
              <SideNav aria-label={t("Settings sections")} xstyle={styles.sideNav}
                header={<HStack gap={2} align="center" paddingInline={2} minHeight={32}><Heading level={4} id={titleId}>{t("Settings")}</Heading></HStack>}>
                {groups.map((group) => (
                  <SideNavSection key={group.label} title={t(group.label)} xstyle={styles.sideNavSection}>
                    {group.panels.map((candidate) => (
                      <SideNavItem key={candidate.id} label={t(candidate.label)} icon={<Icon icon={candidate.icon} size="sm" color="primary" />}
                        isSelected={candidate.id === active} onClick={() => setActive(candidate.id)} />
                    ))}
                  </SideNavSection>
                ))}
              </SideNav>
            }
            content={
              <LayoutContent isScrollable padding={4}>
                <HStack width="100%" justify="end" xstyle={styles.closeAnchor}>
                  <HStack xstyle={styles.closeBacking}><Close onClose={onClose} /></HStack>
                </HStack>
                {body}
              </LayoutContent>
            }
          />
        )}
      </VStack>
    </Dialog>
  );
}
