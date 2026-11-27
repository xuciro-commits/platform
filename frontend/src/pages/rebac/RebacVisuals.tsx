import { Card } from "@astryxdesign/core/Card";
import type { CardVariant } from "@astryxdesign/core/Card";
import { Grid } from "@astryxdesign/core/Grid";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { ProgressBar } from "@astryxdesign/core/ProgressBar";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import type { StatusDotVariant } from "@astryxdesign/core/StatusDot";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { ArrowRightIcon, LockClosedIcon, ShieldCheckIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import type { Api } from "../../host";
import { t } from "../../i18n";
import { PersonLabel } from "../../semantic";
import { motionStyles } from "../motion";
import { grantSourceSummary, layerName, roleTitle, scopeName, scopeRank } from "./shared";

const styles = stylex.create({
  prismArtwork: (dark: boolean) => ({
    width: "min(100%, 380px)",
    aspectRatio: "2 / 1",
    backgroundImage: "url(/media/anatomy/rebac-prism." + (dark ? "dark" : "light") + ".svg)",
    backgroundPosition: "center",
    backgroundSize: "contain",
    backgroundRepeat: "no-repeat",
  }),
  gateArtwork: (dark: boolean) => ({
    width: "min(100%, 340px)",
    aspectRatio: "19 / 11",
    backgroundImage: "url(/media/anatomy/access-gate." + (dark ? "dark" : "light") + ".svg)",
    backgroundPosition: "center",
    backgroundSize: "contain",
    backgroundRepeat: "no-repeat",
  }),
  vaultArtwork: (dark: boolean) => ({
    width: "min(100%, 340px)",
    aspectRatio: "19 / 11",
    backgroundImage: "url(/media/anatomy/rebac-vault." + (dark ? "dark" : "light") + ".svg)",
    backgroundPosition: "center",
    backgroundSize: "contain",
    backgroundRepeat: "no-repeat",
  }),
  meterPill: {
    display: "inline-flex",
    width: "fit-content",
    borderWidth: 1,
    borderStyle: "solid",
    borderColor: "var(--color-neutral)",
    borderRadius: 8,
  },
  meterSpotlit: {
    display: "inline-flex",
    width: "fit-content",
    borderWidth: 1,
    borderStyle: "solid",
    borderColor: "var(--color-accent)",
    backgroundColor: "var(--color-accent-muted)",
    borderRadius: 8,
  },
  bar0: { width: 4, height: 6, borderRadius: 2 },
  bar1: { width: 4, height: 8, borderRadius: 2 },
  bar2: { width: 4, height: 10, borderRadius: 2 },
  bar3: { width: 4, height: 12, borderRadius: 2 },
  bar4: { width: 4, height: 14, borderRadius: 2 },
  barOff: { backgroundColor: "var(--color-neutral)" },
  barGreen: { backgroundColor: "var(--color-success)" },
  barBlue: { backgroundColor: "var(--color-accent)" },
  barAmber: { backgroundColor: "var(--color-warning)" },
  progressBox: { width: 160 },
  stepCode: { width: 28 },
});

const barStyles = [styles.bar0, styles.bar1, styles.bar2, styles.bar3, styles.bar4] as const;

export function AnatomyPlate({
  kind,
  dark,
}: {
  kind: "prism" | "gate" | "vault";
  dark: boolean;
}) {
  const art = kind === "prism" ? styles.prismArtwork(dark) : kind === "gate" ? styles.gateArtwork(dark) : styles.vaultArtwork(dark);
  return <Card variant="transparent" padding={0} xstyle={[art, motionStyles.floatEnter]} />;
}

export type MetricItem = {
  key: string;
  label: string;
  value: string | number;
  caption: string;
  dot: StatusDotVariant;
  pulse?: boolean;
  variant?: CardVariant;
};

export function MetricStrip({ items }: { items: MetricItem[] }) {
  return (
    <Grid columns={{ minWidth: 185, max: items.length }} gap={3}>
      {items.map((item) => (
        <Card key={item.key} variant={item.variant ?? "muted"} padding={3}>
          <VStack gap={1}>
            <HStack gap={2} vAlign="center" hAlign="between">
              <Text type="supporting" color="secondary" maxLines={1}>{item.label}</Text>
              <StatusDot variant={item.dot} isPulsing={item.pulse} label={item.label} />
            </HStack>
            <Heading level={2}>{String(item.value)}</Heading>
            <Text type="supporting" color="secondary" maxLines={1}>{item.caption}</Text>
          </VStack>
        </Card>
      ))}
    </Grid>
  );
}

export function ScopeDepthMeter({
  scope,
  roleLabel,
  isSpotlit = false,
}: {
  scope?: string;
  roleLabel?: string;
  isSpotlit?: boolean;
}) {
  const rank = scopeRank(scope);
  const fillStyle = rank >= 5 ? styles.barGreen : rank >= 3 ? styles.barBlue : styles.barAmber;
  return (
    <Card
      width="fit-content"
      variant="transparent"
      padding={1.5}
      xstyle={isSpotlit ? styles.meterSpotlit : styles.meterPill}
    >
      <HStack gap={1.5} vAlign="center">
        <HStack gap={0.5} vAlign="end">
          {[0, 1, 2, 3, 4].map((idx) => (
            <Card
              key={idx}
              variant="transparent"
              padding={0}
              xstyle={[barStyles[idx], idx < rank ? fillStyle : styles.barOff]}
            />
          ))}
        </HStack>
        {roleLabel && <Text type="supporting" weight="semibold">{roleLabel}</Text>}
        <Text type="supporting" color="secondary">{scopeName(scope)}</Text>
      </HStack>
    </Card>
  );
}

export function ProvenanceFlowCard({
  memberId,
  memberDisplay,
  personName,
  isAgent,
  units,
  grants,
  markings,
  memberName,
  unitName,
}: {
  memberId: string;
  memberDisplay: string;
  personName: string;
  isAgent?: boolean;
  units: string[];
  grants: Api.Grant[];
  markings: string[];
  memberName: (id?: string) => string;
  unitName: (id: string) => string;
}) {
  return (
    <Card padding={4}>
      <VStack gap={3}>
        <HStack gap={2} vAlign="center" hAlign="between" wrap="wrap">
          <Heading level={4}>{t("Subject relationship provenance")}</Heading>
          <Text type="supporting" color="secondary">{t("Identity → Person → Organization unit → Active grants & clearances")}</Text>
        </HStack>
        <Grid columns={{ minWidth: 180, max: 4, repeat: "fit" }} gap={2}>
          <Card variant="muted" padding={3}>
            <VStack gap={1.5}>
              <HStack gap={1.5} vAlign="center" hAlign="between">
                <Text type="code" color="secondary">{"01 · " + t("Sign-in identity")}</Text>
                <Icon icon={ArrowRightIcon} size="sm" color="secondary" />
              </HStack>
              <PersonLabel id={memberId} name={memberDisplay} agent={isAgent} weight="medium" />
              <Text type="code" color="secondary" maxLines={1}>{memberId}</Text>
            </VStack>
          </Card>

          <Card variant="muted" padding={3}>
            <VStack gap={1.5}>
              <HStack gap={1.5} vAlign="center" hAlign="between">
                <Text type="code" color="secondary">{"02 · " + t("Person link")}</Text>
                <Icon icon={ArrowRightIcon} size="sm" color="secondary" />
              </HStack>
              {personName ? (
                <>
                  <Text weight="medium" maxLines={1}>{personName}</Text>
                  <HStack gap={1} wrap="wrap"><Token size="sm" color="teal" label={t("Signs in as")} /></HStack>
                </>
              ) : (
                <Text type="supporting" color="secondary">{t("Service or unlinked account")}</Text>
              )}
            </VStack>
          </Card>

          <Card variant="muted" padding={3}>
            <VStack gap={1.5}>
              <HStack gap={1.5} vAlign="center" hAlign="between">
                <Text type="code" color="secondary">{"03 · " + t("Organization units")}</Text>
                <Icon icon={ArrowRightIcon} size="sm" color="secondary" />
              </HStack>
              {units.length > 0 ? (
                <HStack gap={1} wrap="wrap">
                  {units.map((u) => <Token key={u} size="sm" color="blue" label={u} />)}
                </HStack>
              ) : (
                <Text type="supporting" color="secondary">{t("Not placed in any unit")}</Text>
              )}
            </VStack>
          </Card>

          <Card variant="muted" padding={3}>
            <VStack gap={1.5}>
              <HStack gap={1.5} vAlign="center" hAlign="between">
                <Text type="code" color="secondary">{"04 · " + t("Active role grants")}</Text>
                <Icon icon={ShieldCheckIcon} size="sm" color="secondary" />
              </HStack>
              {grants.length > 0 ? (
                <HStack gap={1} wrap="wrap">
                  {grants.slice(0, 4).map((grant, index) => {
                    const origin = grantSourceSummary(grant, memberName);
                    return (
                      <Token
                        key={`${grant.app}:${grant.role}:${index}`}
                        size="sm"
                        color={origin.kind === "delegated" ? "orange" : origin.kind === "team" ? "purple" : "default"}
                        label={`${t(grant.app)}:${roleTitle(grant.role)}${grant.unit ? `@${unitName(grant.unit)}` : ""}`}
                      />
                    );
                  })}
                  {grants.length > 4 && <Token size="sm" color="gray" label={`+${grants.length - 4}`} />}
                </HStack>
              ) : (
                <Text type="supporting" color="secondary">{t("No active role grants")}</Text>
              )}
              {markings.length > 0 && (
                <HStack gap={1} wrap="wrap">
                  {markings.map((marking) => <Token key={marking} size="sm" color="teal" label={t(marking)} />)}
                </HStack>
              )}
            </VStack>
          </Card>
        </Grid>
      </VStack>
    </Card>
  );
}

function gateExplanation(layer: Api.Layer, withheldCount: number): string {
  if (layer.allowed) {
    switch (layer.name) {
      case "tenant":
        return t("Verified within current workspace boundary");
      case "principal":
        return t("Principal account is active and in good standing");
      case "deny":
        return t("No explicit deny guardrail policy matched");
      case "verb":
        return layer.policy
          ? t("Explicit allow policy grants this operation")
          : t("Assigned role grants this operation");
      case "row":
        return t("Target record matches the subject's ReBAC relationship scope");
      case "fields":
        return withheldCount > 0
          ? t("Record readable with sensitive columns masked")
          : t("All involved fields permitted for this role");
      case "derived":
        return t("Upstream lineage & source data closure verified");
      case "markings":
        return t("Subject holds required classifications & compartment clearances");
      case "allow":
        return t("All 9 authorization gates satisfied");
    }
  }
  return t(layer.reason);
}

export function GatePipelineTrace({ verdict }: { verdict: Api.Verdict }) {
  const total = verdict.layers.length;
  const passed = verdict.layers.filter((layer) => layer.allowed).length;
  return (
    <VStack gap={3} xstyle={motionStyles.contentEnter}>
      <Card variant={verdict.allow ? "green" : "red"} padding={4}>
        <HStack gap={3} vAlign="center" hAlign="between" wrap="wrap">
          <VStack gap={1}>
            <HStack gap={2} vAlign="center" wrap="wrap">
              <StatusDot
                variant={verdict.allow ? "success" : "error"}
                isPulsing
                label={verdict.allow ? t("Access allowed") : t("Access denied")}
              />
              <Heading level={3}>{verdict.allow ? t("Access allowed") : t("Access denied")}</Heading>
              {verdict.role && <Token size="sm" color="blue" label={`${t("Matched role")}: ${roleTitle(verdict.role)}`} />}
              {verdict.policy && <Token size="sm" color="orange" label={`${t("Matched policy")}: ${verdict.policy}`} />}
            </HStack>
            <Text type="body">{t(verdict.reason)}</Text>
          </VStack>

          {total > 0 && (
            <VStack gap={1} hAlign="end">
              <Text type="code" hasTabularNumbers>
                {t("{passed} of {total} security gates passed").replace("{passed}", String(passed)).replace("{total}", String(total))}
              </Text>
              <VStack xstyle={styles.progressBox}>
                <ProgressBar
                  value={passed}
                  max={Math.max(1, total)}
                  variant={verdict.allow ? "success" : "error"}
                  label={t("Passed security gates")}
                  isLabelHidden
                />
              </VStack>
            </VStack>
          )}
        </HStack>
      </Card>

      {(verdict.withheld?.length ?? 0) > 0 && (
        <Card variant="orange" padding={3}>
          <HStack gap={2} vAlign="center" wrap="wrap">
            <Icon icon={LockClosedIcon} size="sm" />
            <Text weight="medium">{t("Restricted fields withheld on this record")}:</Text>
            {(verdict.withheld ?? []).map((field) => (
              <Token key={field} size="sm" color="orange" label={field} />
            ))}
          </HStack>
        </Card>
      )}

      {verdict.layers.length > 0 && (
        <VStack gap={1.5}>
          {verdict.layers.map((layer, index) => {
            const code = String(index + 1).padStart(2, "0");
            const cardVariant: CardVariant = !layer.allowed
              ? "red"
              : layer.policy
                ? "orange"
                : layer.role
                  ? "blue"
                  : "muted";
            return (
              <Card key={`${index}:${layer.name}`} variant={cardVariant} padding={3}>
                <HStack gap={3} vAlign="center" hAlign="between" wrap="wrap">
                  <HStack gap={2} vAlign="center" wrap="wrap">
                    <VStack xstyle={styles.stepCode}>
                      <Text type="code" color="secondary" hasTabularNumbers>{code}</Text>
                    </VStack>
                    <StatusDot
                      variant={layer.allowed ? "success" : "error"}
                      label={layer.allowed ? t("Passed") : t("Blocked")}
                    />
                    <Text weight="semibold">{layerName(layer.name)}</Text>
                    {layer.policy && <Token size="sm" color="orange" label={`${t("Policy")}: ${layer.policy}`} />}
                    {layer.role && <Token size="sm" color="blue" label={`${t("Role")}: ${roleTitle(layer.role)}`} />}
                  </HStack>
                  <StackItem size="fill">
                    <HStack hAlign="end">
                      <Text type="supporting" color="secondary" maxLines={2}>{gateExplanation(layer, verdict.withheld?.length ?? 0)}</Text>
                    </HStack>
                  </StackItem>
                </HStack>
              </Card>
            );
          })}
        </VStack>
      )}
    </VStack>
  );
}
