import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { HStack, Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Heading, Text } from "@astryxdesign/core/Text";
import { useToast } from "@astryxdesign/core/Toast";
import { useState } from "react";
import { useRecordHref } from "../apps/entries";
import { type Api, type Read, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import { ActionDialog } from "./ActionDialog";
import { AvatarPicker } from "./AvatarPicker";
import { elementHref, elementType, personStereotype } from "./enterprise";
import { PageTitle, useGutter } from "./frame";
import { ListView } from "./ListPage";
import { MemberInput, PersonLabel } from "./MemberField";
import { present } from "./values";

type Span = { from?: string; until?: string; periods?: Api.Period[] };
type Person = { id: string; name: string; account: string; units: string };
type Directory = { model: Api.EnterpriseModel; names: Record<string, string> };
type Write = { (): Promise<void> };

const signsInAs = "SignsInAs";
const membership = "ActualOrganizationRole";
const today = () => new Date().toISOString().slice(0, 10);
const alive = (span: Span, day: string) =>
  (span.periods?.length ? span.periods : [span]).some((period) => (!period.from || period.from <= day) && (!period.until || day < period.until));

function useDirectory(): Read<Directory> {
  const model = useRead<Api.EnterpriseModel>("/v1/enterprise");
  const members = useRead<Api.MemberOption[]>("/v1/member-directory");
  if (model.status !== "ready") return model;
  const names = members.status === "ready" ? Object.fromEntries(members.data.map((member) => [member.id, member.name])) : {};
  return { status: "ready", path: model.path, data: { model: model.data, names } };
}

function accountOf(model: Api.EnterpriseModel, person: string, day: string) {
  return model.relationships.find((relation) => relation.stereotype === signsInAs && relation.target === person && alive(relation, day));
}

function unitsOf(model: Api.EnterpriseModel, person: string, account: string | undefined, day: string) {
  return model.relationships.filter((relation) => relation.stereotype === membership && alive(relation, day) &&
    (relation.source === person || (account !== undefined && relation.source === `member:${account}`)));
}

function people({ model, names }: Directory): Person[] {
  const day = today();
  const nameOf = (id: string) => model.elements.find((element) => element.id === id)?.name ?? id;
  return model.elements.filter((element) => element.stereotype === personStereotype && !element.deleted).map((element) => {
    const account = accountOf(model, element.id, day)?.source.replace(/^member:/, "");
    return {
      id: element.id,
      name: element.name,
      account: account ? names[account] ?? account : "",
      units: unitsOf(model, element.id, account, day).map((relation) => nameOf(relation.target)).join(", "),
    };
  });
}

export function PeopleList() {
  const read = useDirectory();
  const actions = useRead<Api.Action[]>("/v1/actions");
  const add = actions.status === "ready" ? actions.data.find((action) => action.schema === "enterprise.element.add") : undefined;
  const [creating, setCreating] = useState(false);
  return (
    <>
      <ListView
        read={read}
        primary={add && <Button label={t("New")} variant="primary" onClick={() => setCreating(true)} />}
        definition={{
          title: "People",
          about: "The people in your organization. An account signs in as one of them; applications attach their own details to a person.",
          rows: people,
          id: (person) => person.id,
          link: (person) => elementHref(person.id),
          columns: [
            { key: "name", header: "Name", kind: "person", text: (person) => person.name },
            { key: "account", header: "Sign-in account", width: 200, kind: "person", text: (person) => person.account },
            { key: "units", header: "Units", text: (person) => person.units },
          ],
        }}
      />
      {creating && add && (
        <ActionDialog action={add} heading={t("New person")} confirm={t("Create")} fixed={{ stereotype: personStereotype }} only={["name"]}
          onClose={() => setCreating(false)} onDone={() => setCreating(false)}
          perform={async (payload) => void (await send(add, `person-${crypto.randomUUID().slice(0, 8)}`, payload))} />
      )}
    </>
  );
}

function AccountSection({ person, model, names, actions }: { person: string; model: Api.EnterpriseModel; names: Record<string, string>; actions: Api.Action[] }) {
  const toast = useToast();
  const [picked, setPicked] = useState("");
  const [busy, setBusy] = useState(false);
  const signs = accountOf(model, person, today());
  const account = signs?.source.replace(/^member:/, "");
  const relate = actions.find((action) => action.schema === "enterprise.relationship.add");
  const end = actions.find((action) => action.schema === "enterprise.relationship.end");
  const run = async (write: Write) => {
    setBusy(true);
    try {
      await write();
      setPicked("");
    } catch (problem) {
      toast({ body: reasonOf(problem), type: "error" });
    } finally {
      setBusy(false);
    }
  };
  return (
    <VStack gap={3}>
      <Heading level={2}>{t("Sign-in account")}</Heading>
      {account ? (
        <HStack gap={3} vAlign="center">
          <PersonLabel id={account} name={names[account] ?? account} />
          {end && signs && <Button label={t("Unlink account")} variant="secondary" size="sm" isDisabled={busy}
            onClick={() => void run(async () => { await send(end, signs.id, { until: today() }); })} />}
        </HStack>
      ) : (
        <Text type="supporting" color="secondary">{t("No account signs in as this person.")}</Text>
      )}
      {!account && relate && (
        <HStack gap={2} vAlign="end">
          <MemberInput id={picked} label={t("Account that signs in as this person")} onChange={setPicked} />
          <Button label={t("Link account")} variant="secondary" isDisabled={!picked || busy}
            onClick={() => void run(async () => { await send(relate, `signs-${crypto.randomUUID().slice(0, 8)}`, { stereotype: signsInAs, source: `member:${picked}`, target: person }); })} />
        </HStack>
      )}
    </VStack>
  );
}

function FacetSection({ type, id, entity }: { type: string; id: string; entity: Api.EntityInfo }) {
  const href = useRecordHref();
  const read = useRead<Api.RecordView>(`/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}`);
  const values = read.status === "ready" ? read.data.values : undefined;
  const fields = entity.fields.filter((field) => field.name !== "base" && values?.[field.name] != null && values[field.name] !== "");
  return (
    <VStack gap={3}>
      <HStack gap={3} vAlign="center">
        <Heading level={2}>{t(entity.title)}</Heading>
        <Link href={href(type, id)}>{t("Open record")}</Link>
      </HStack>
      {read.status === "loading" && <Skeleton height={48} />}
      {values && (fields.length ? (
        <MetadataList>
          {fields.map((field) => <MetadataListItem key={field.name} label={t(field.title)}>{present(field, values[field.name])}</MetadataListItem>)}
        </MetadataList>
      ) : <Text type="supporting" color="secondary">{t("Nothing filled in yet.")}</Text>)}
    </VStack>
  );
}

export function PersonPage({ id }: { id: string }) {
  const gutter = useGutter();
  const href = useRecordHref();
  const directory = useDirectory();
  const references = useRead<Api.EnterpriseReferenceGroup[]>(`/v1/enterprise-references?element=${encodeURIComponent(id)}`);
  const entities = useRead<Api.EntityInfo[]>("/v1/entities");
  const actions = useRead<Api.Action[]>("/v1/actions");
  if (directory.status === "loading") return <Skeleton height={240} />;
  const model = directory.status === "ready" ? directory.data.model : undefined;
  const element = model?.elements.find((candidate) => candidate.id === id && !candidate.deleted);
  if (!model || !element) {
    return <EmptyState headingLevel={1} title={t("This person is not in the organization")} description={directory.status === "ready" ? undefined : directory.reason} />;
  }
  const day = today();
  const account = accountOf(model, id, day)?.source.replace(/^member:/, "");
  const nameOf = (unit: string) => model.elements.find((candidate) => candidate.id === unit)?.name ?? unit;
  const units = unitsOf(model, id, account, day);
  const groups = references.status === "ready" ? references.data : [];
  const described = entities.status === "ready" ? entities.data : [];
  const facetOf = (group: Api.EnterpriseReferenceGroup) => {
    const entity = described.find((candidate) => candidate.type === group.type);
    return entity?.extends === elementType && group.field === "base" ? entity : undefined;
  };
  const others = groups.filter((group) => !facetOf(group) && group.records.length > 0);
  const edit = actions.status === "ready" ? actions.data.find((action) => action.schema === "enterprise.element.edit") : undefined;
  const elementAvatar = (element as { avatar?: string }).avatar;
  return (
    <Layout
      height="fill"
      contentWidth={1260}
      header={
        <LayoutHeader padding={0} hasDivider>
          <VStack padding={gutter} paddingBlockStart={3} paddingBlockEnd={4}>
            <PageTitle title={element.name} back={{ href: "/admin/people", label: t("People") }} />
          </VStack>
        </LayoutHeader>
      }
      content={
        <LayoutContent padding={gutter} role="main">
          <VStack gap={6}>
            <AvatarPicker
              name={element.name}
              avatar={elementAvatar}
              seed={id}
              isDisabled={!edit}
              onChange={async (next) => { if (edit) await send(edit, id, { avatar: next }); }}
            />
            {directory.status === "ready" && actions.status === "ready" && (
              <AccountSection person={id} model={model} names={directory.data.names} actions={actions.data} />
            )}
            <VStack gap={3}>
              <Heading level={2}>{t("Units")}</Heading>
              {units.length ? (
                <MetadataList>
                  {units.map((relation) => <MetadataListItem key={relation.id} label={nameOf(relation.target)}>{t(relation.role ?? "")}</MetadataListItem>)}
                </MetadataList>
              ) : <Text type="supporting" color="secondary">{t("Not placed in any unit.")}</Text>}
            </VStack>
            {groups.flatMap((group) => {
              const entity = facetOf(group);
              return entity ? group.records.map((record) => <FacetSection key={`${record.type}/${record.id}`} type={record.type} id={record.id} entity={entity} />) : [];
            })}
            {others.length > 0 && (
              <VStack gap={3}>
                <Heading level={2}>{t("Named in")}</Heading>
                <MetadataList>
                  {others.map((group) => (
                    <MetadataListItem key={`${group.type}/${group.field}`} label={`${t(group.title)} · ${t(group.fieldTitle)}`}>
                      <HStack gap={2} wrap="wrap">
                        {group.records.map((record) => <Link key={record.id} href={href(record.type, record.id)}>{record.name || record.id}</Link>)}
                      </HStack>
                    </MetadataListItem>
                  ))}
                </MetadataList>
              </VStack>
            )}
          </VStack>
        </LayoutContent>
      }
    />
  );
}
