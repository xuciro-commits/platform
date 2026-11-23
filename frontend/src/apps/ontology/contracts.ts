import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.contract",
  project: true,
  title: "Capability contracts",
  about: "A capability contract is the versioned interface of a capability: its actions, reads and events. Objects implement it, and the tenant chooses which implementation serves it.",
});
