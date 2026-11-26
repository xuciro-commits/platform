import { Card } from "@astryxdesign/core/Card";
import { Grid } from "@astryxdesign/core/Grid";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { List, ListItem } from "@astryxdesign/core/List";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { useMemo } from "react";
import { type Api, useRead } from "../../host";
import { t } from "../../i18n";
import { elementHref } from "../enterprise";
import { PersonAvatar } from "../MemberField";
import { roleTitle, spanAlive, todayISO, useMemberDirectoryMap } from "./shared";

export function UnitAccessPanel({ unitId, org }: { unitId: string; org: Api.OrgSeed }) {
  const enterprise = useRead<Api.EnterpriseModel>("/v1/enterprise");
  const members = useRead<Api.MemberView[]>("/v1/members");
  const access = useRead<Api.AccessConfig>("/v1/access");
  const { nameOf: memberName } = useMemberDirectoryMap();

  const unit = org.units.find((candidate) => candidate.id === unitId);
  const today = todayISO();

  const subtreeIds = useMemo(() => {
    const ids = new Set<string>([unitId]);
    let changed = true;
    while (changed) {
      changed = false;
      for (const edge of org.edges) {
        if (!spanAlive(edge, today)) continue;
        if (edge.parent && ids.has(edge.parent) && !ids.has(edge.unit)) {
          ids.add(edge.unit);
          changed = true;
        }
      }
    }
    return ids;
  }, [org.edges, unitId, today]);

  const unitNameOf = (id: string) => org.units.find((u) => u.id === id)?.name || id;

  const unitPeople = useMemo(() => {
    const rows: { id: string; avatarId: string; avatar?: string; label: string; href?: string; role: string; subUnit?: string; until?: string }[] = [];
    const seen = new Set<string>();
    const elements = enterprise.status === "ready" ? enterprise.data.elements : [];
    const relations = enterprise.status === "ready" ? enterprise.data.relationships : [];
    const findElement = (id: string) => elements.find((el) => el.id === id);

    const addParty = (rawParty: string, targetUnit: string, role?: string, until?: string) => {
      const party = rawParty.startsWith("unit:") ? rawParty.slice(5) : rawParty;
      if (!rawParty.startsWith("member:") && relations.some((r) => r.stereotype === "SignsInAs" && r.target === party && spanAlive(r, today) && org.memberships.some((m) => m.party === r.source && m.unit === targetUnit && spanAlive(m, today)))) {
        return;
      }
      const key = `${party}:${targetUnit}`;
      if (seen.has(key)) return;
      seen.add(key);
      const subUnit = targetUnit !== unitId ? unitNameOf(targetUnit) : undefined;
      if (party.startsWith("member:")) {
        const memId = party.slice(7);
        rows.push({
          id: key,
          avatarId: memId,
          label: memberName(memId),
          href: `/admin/members?member=${encodeURIComponent(memId)}`,
          role: role || "member",
          subUnit,
          until,
        });
      } else {
        const person = findElement(party);
        if (person?.deleted) return;
        const signs = relations.find((r) => r.stereotype === "SignsInAs" && r.target === party && spanAlive(r, today));
        const linkedMember = signs?.source.startsWith("member:") ? signs.source.slice(7) : "";
        rows.push({
          id: key,
          avatarId: linkedMember || party,
          avatar: (person as { avatar?: string } | undefined)?.avatar,
          label: person?.name || party,
          href: elementHref(party),
          role: role || "member",
          subUnit,
          until,
        });
      }
    };

    for (const m of org.memberships) {
      if (subtreeIds.has(m.unit) && spanAlive(m, today)) addParty(m.party, m.unit, m.role, m.until);
    }
    for (const rel of relations) {
      if (rel.stereotype === "ActualOrganizationRole" && subtreeIds.has(rel.target) && spanAlive(rel, today)) {
        addParty(rel.source, rel.target, rel.role, rel.until);
      }
    }
    return rows;
  }, [org, unitId, subtreeIds, enterprise, memberName, today]);

  const scopedGrants = useMemo(() => {
    const rows: { id: string; holder: string; kind: "member" | "team"; app: string; role: string; subUnit?: string; until?: string }[] = [];
    if (members.status === "ready") {
      for (const mem of members.data) {
        for (const grant of mem.grants ?? []) {
          if (!grant.unit || !subtreeIds.has(grant.unit) || (grant.by ?? "").startsWith("team:")) continue;
          rows.push({
            id: `m:${mem.id}:${grant.app}:${grant.role}:${grant.unit}`,
            holder: mem.profile?.displayName || memberName(mem.id) || mem.id,
            kind: "member",
            app: grant.app,
            role: grant.role,
            subUnit: grant.unit !== unitId ? unitNameOf(grant.unit) : undefined,
            until: grant.until,
          });
        }
      }
    }
    if (access.status === "ready") {
      for (const team of access.data.teams) {
        for (const grant of team.grants ?? []) {
          if (!grant.unit || !subtreeIds.has(grant.unit)) continue;
          rows.push({
            id: `t:${team.id}:${grant.app}:${grant.role}:${grant.unit}`,
            holder: team.name,
            kind: "team",
            app: grant.app,
            role: grant.role,
            subUnit: grant.unit !== unitId ? unitNameOf(grant.unit) : undefined,
            until: grant.until,
          });
        }
      }
    }
    return rows;
  }, [members, access, unitId, subtreeIds, memberName]);

  if (!unit) return null;

  return (
    <VStack gap={4}>
      <Card variant="muted" padding={3}>
        <VStack gap={2}>
          <HStack gap={2} vAlign="center" hAlign="between" wrap="wrap">
            <Heading level={3}>{unit.name || unit.id}</Heading>
            {unit.kind && <Token size="sm" color="blue" label={t(unit.kind)} />}
          </HStack>
          <Text type="code" color="secondary">{unit.id}</Text>
          <Grid columns={2} gap={2}>
            <Card padding={2}>
              <VStack gap={0.5}>
                <Text type="supporting" color="secondary">{t("Placed people")}</Text>
                <Heading level={3}>{String(unitPeople.length)}</Heading>
              </VStack>
            </Card>
            <Card padding={2}>
              <VStack gap={0.5}>
                <Text type="supporting" color="secondary">{t("Scoped grants")}</Text>
                <Heading level={3}>{String(scopedGrants.length)}</Heading>
              </VStack>
            </Card>
          </Grid>
        </VStack>
      </Card>

      <VStack gap={2}>
        <Heading level={4}>{t("Placed people & members")}</Heading>
        {unitPeople.length > 0 ? (
          <List hasDividers>
            {unitPeople.map((person) => (
              <ListItem
                key={person.id}
                startContent={<PersonAvatar id={person.avatarId} avatar={person.avatar} name={person.label} size="sm" />}
                label={person.label}
                description={[person.subUnit, t(person.role)].filter(Boolean).join(" · ")}
                endContent={
                  <HStack gap={1.5} vAlign="center">
                    {person.until && <Token size="sm" color="blue" label={t("Until {date}").replace("{date}", person.until)} />}
                    {person.href && <Link href={person.href}>{t("View")}</Link>}
                  </HStack>
                }
              />
            ))}
          </List>
        ) : (
          <Text type="supporting" color="secondary">{t("No people directly placed in this unit.")}</Text>
        )}
      </VStack>

      <VStack gap={2}>
        <Heading level={4}>{t("Unit-scoped role grants")}</Heading>
        {scopedGrants.length > 0 ? (
          <List hasDividers>
            {scopedGrants.map((item) => (
              <ListItem
                key={item.id}
                label={item.holder}
                description={[item.subUnit, `${item.app} · ${roleTitle(item.role)}`].filter(Boolean).join(" · ")}
                endContent={
                  <HStack gap={1} vAlign="center">
                    <Token size="sm" color={item.kind === "team" ? "purple" : "default"} label={t(item.kind === "team" ? "Team" : "Member")} />
                    {item.until && <Token size="sm" color="blue" label={item.until} />}
                  </HStack>
                }
              />
            ))}
          </List>
        ) : (
          <Text type="supporting" color="secondary">{t("No role grants are explicitly restricted to this unit.")}</Text>
        )}
      </VStack>
    </VStack>
  );
}
