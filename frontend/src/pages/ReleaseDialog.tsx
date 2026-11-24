import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { Token } from "@astryxdesign/core/Token";
import { useEffect, useMemo, useState } from "react";
import { type Api, request, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";

type Root = { app: string; kind: string; name: string };
type Product = { id: string; name: string; title: string; project: string; roots: Root[] };
type Project = { id: string; name: string; title?: string };
type Version = { product: string; version: string };
type Choice = { product?: Product; project: Project };

const next = (versions: string[]) => {
  const parsed = versions.map((version) => version.split(".").map(Number)).filter((parts) => parts.length === 3 && parts.every(Number.isInteger));
  if (!parsed.length) return "0.1.0";
  const [major, minor, patch] = parsed.sort((a, b) => a[0] - b[0] || a[1] - b[1] || a[2] - b[2]).at(-1)!;
  return `${major}.${minor}.${patch + 1}`;
};

async function draftsOf(roots: Root[]) {
  return Promise.all(roots.map(async (root) => {
    const short = root.name.startsWith(`${root.app}.`) ? root.name.slice(root.app.length + 1) : root.name;
    const page = await request<Api.RecordPage>(`/v1/records/build.${root.kind}?${new URLSearchParams({ search: short, limit: "20" })}`);
    const found = (page.records as { id: string; name?: string }[]).find((record) => record.name === short || record.name === root.name);
    return { kind: root.kind, id: found?.id ?? short };
  }));
}

export function ReleaseDialog({ onClose, onDone }: { onClose: () => void; onDone: (id: string) => void }) {
  const products = useRead<Api.RecordPage>("/v1/records/build.product?limit=200");
  const projects = useRead<Api.RecordPage>("/v1/records/build.app?limit=200");
  const versions = useRead<Api.RecordPage>("/v1/records/build.productversion?limit=500");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const entities = useRead<Api.EntityInfo[]>("/v1/entities");
  const choices = useMemo(() => {
    if (products.status !== "ready" || projects.status !== "ready") return new Map<string, Choice>();
    const known = products.data.records as Product[];
    const all = projects.data.records as Project[];
    const project = (name: string) => all.find((candidate) => candidate.name === name) ?? { id: name, name };
    return new Map<string, Choice>([
      ...known.map((product) => [`product:${product.id}`, { product, project: project(product.project) }] as const),
      ...all.filter((candidate) => !known.some((product) => product.project === candidate.name)).map((candidate) => [`project:${candidate.id}`, { project: candidate }] as const),
    ]);
  }, [products, projects]);
  const [picked, setPicked] = useState("");
  const [version, setVersion] = useState("");
  const [preview, setPreview] = useState<{ drafts: Api.JointDraftRef[]; answer: Api.ReleasePreview }>();
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const choice = choices.get(picked);
  const name = choice?.product?.name ?? choice?.project.name ?? "";
  const roots = useMemo(() => choice?.product?.roots ?? (choice ? [{ app: "build", kind: "app", name: choice.project.name }] : []), [choice]);

  useEffect(() => {
    if (!picked && choices.size === 1) setPicked([...choices.keys()][0]);
  }, [choices, picked]);

  useEffect(() => {
    if (!name || versions.status !== "ready") return;
    setVersion(next((versions.data.records as Version[]).filter((record) => record.product === name).map((record) => record.version)));
  }, [name, versions]);

  useEffect(() => {
    setPreview(undefined);
    setFailure("");
    if (!roots.length) return;
    let current = true;
    draftsOf(roots)
      .then(async (drafts) => ({ drafts, answer: await request<Api.ReleasePreview>("/v1/releases/preview", { body: { drafts } }) }))
      .then((result) => current && setPreview(result), (problem) => current && setFailure(reasonOf(problem)));
    return () => { current = false; };
  }, [roots]);

  const answer = preview?.answer;
  const issues = [...(answer?.diagnostic ? [answer.diagnostic] : []), ...(answer?.draftReviews ?? []).flatMap((review) => review.issues)];
  const action = (schema: string) => (catalog.status === "ready" ? catalog.data.find((candidate) => candidate.schema === schema) : undefined);
  const mark = (ref: Api.AssetRef) => {
    const same = (other: Api.AssetRef) => other.app === ref.app && other.kind === ref.kind && other.name === ref.name;
    if (answer?.added.some(same)) return <Token label={t("Added")} color="green" size="sm" />;
    if (answer?.changed.some(same)) return <Token label={t("Changed")} color="blue" size="sm" />;
    return undefined;
  };
  const titled = (ref: Api.AssetRef) => {
    const title = ref.kind === "action" ? answer?.candidateActions.find((candidate) => candidate.schema === ref.name)?.title
      : ref.kind === "object" && entities.status === "ready" ? entities.data.find((entity) => entity.type === ref.name)?.title
      : ref.kind === "app" && projects.status === "ready" ? (projects.data.records as Project[]).find((project) => project.name === ref.name)?.title : undefined;
    return { label: title ? t(title) : ref.name, description: `${t(ref.kind)} · ${ref.name}` };
  };
  const ready = Boolean(choice && answer?.candidateId && /^\d+\.\d+\.\d+$/.test(version) && !issues.length && action("build.productversion.create"));

  const publish = async () => {
    if (!choice || !preview?.answer.candidateId) return;
    setBusy(true);
    setFailure("");
    try {
      if (!choice.product) {
        const create = action("build.product.create");
        if (!create) throw new Error(t("Publishing is unavailable."));
        await send(create, name, { name, title: choice.project.title || name, project: choice.project.name, roots }, 0);
      }
      const candidate = preview.answer.candidateId;
      await request<Api.ReleaseSaved>("/v1/releases/candidates", { body: { drafts: preview.drafts, candidateId: candidate, key: `${name}@${version}` } });
      const id = `${name}@${version}`;
      await send(action("build.productversion.create")!, id, { product: name, version, candidate }, 0);
      onDone(id);
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  const options = [...choices.entries()].map(([value, entry]) => ({
    value,
    label: entry.product ? `${entry.product.title || entry.product.name} · ${entry.project.title || entry.project.name}` : `${entry.project.title || entry.project.name} · ${t(action("build.product.create")?.title ?? "")}`,
  }));
  const title = t(action("build.productversion.create")?.title ?? "");

  return (
    <Dialog isOpen onOpenChange={() => !busy && onClose()} width={760} purpose="form">
      <Layout
        padding={4}
        header={<DialogHeader title={title} onOpenChange={() => !busy && onClose()} />}
        content={
          <LayoutContent>
            <VStack gap={4}>
              <Selector label={t("Product")} isRequired value={picked} onChange={setPicked} options={options} />
              <TextInput label={t("Version")} isRequired value={version} onChange={setVersion} placeholder="0.1.0" />
              {failure && <Banner status="error" title={t("Not done")} description={failure} />}
              {issues.length > 0 && <Banner status="error" title={t("Publishing is unavailable.")} description={issues.join(" · ")} />}
              {choice && !answer && !failure && <Text type="supporting">{t("Loading…")}</Text>}
              {answer && (
                <List header={`${name} ${version} · ${answer.included.length} · ${t("Added")} ${answer.added.length} · ${t("Changed")} ${answer.changed.length} · ${t("Removed")} ${answer.removed.length}`} hasDividers>
                  {answer.included.map((ref) => (
                    <ListItem key={`${ref.kind}/${ref.name}`} {...titled(ref)} endContent={mark(ref)} />
                  ))}
                  {answer.removed.map((ref) => (
                    <ListItem key={`removed/${ref.kind}/${ref.name}`} {...titled(ref)} endContent={<Token label={t("Removed")} color="red" size="sm" />} />
                  ))}
                </List>
              )}
              <Text type="supporting" color="secondary">{t("Once published, this product version is immutable and cannot be overwritten.")}</Text>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill" />
              <Button label={t("Cancel")} variant="secondary" isDisabled={busy} onClick={onClose} />
              <Button label={title} variant="primary" isLoading={busy} isDisabled={busy || !ready} onClick={() => void publish()} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
