import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.capabilitycall",
  title: "Capability calls",
  about: "Each call an application made through a capability that a connector serves: what was sent, and what the connector answered. A call without an answer is never resent automatically; settle it in Integrations.",
});
