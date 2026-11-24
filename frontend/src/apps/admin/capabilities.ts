import { createElement } from "react";
import { CapabilitiesList, CapabilityPage } from "../../pages/Capabilities";
import { usePath } from "../../router";

export default function CapabilitiesEntry() {
  const id = usePath().split("/").slice(3).join("/");
  return id ? createElement(CapabilityPage, { id: decodeURIComponent(id) }) : createElement(CapabilitiesList);
}
