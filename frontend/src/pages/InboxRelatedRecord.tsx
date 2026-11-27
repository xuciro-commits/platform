import { Text } from "@astryxdesign/core/Text";
import { type Api, useRead } from "../host";
import { ReferenceLink } from "./ReferenceField";

export function RelatedRecord({ reference }: { reference: string }) {
  const [type, id] = reference.split("/");
  const types = useRead<Api.EntityInfo[]>("/v1/entities");
  const record = useRead<Api.RecordView>(`/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}`);
  const entity = types.status === "ready" ? types.data.find((candidate) => candidate.type === type) : undefined;
  const values = record.status === "ready" ? record.data.values : undefined;
  const name = entity?.display && values?.[entity.display] != null ? String(values[entity.display]) : id;
  if (record.status !== "ready") return <Text type="body">{id}</Text>;
  return <ReferenceLink type={type} id={id} name={name} />;
}
