import { Button, t } from "@platform/ui";

export type ButtonPorts = { title?: string; enabled?: boolean; onClick?: () => void };
/** Click is a typed callback supplied by the original finite event owner. */
export function ButtonRenderer({title,enabled,onClick}:ButtonPorts) {
  return <Button onClick={onClick} disabled={!onClick||enabled===false}>{title||t("Button")}</Button>;
}
