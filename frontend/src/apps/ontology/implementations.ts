import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.implementation",
  project: true,
  title: "Capability implementations",
  about: "A capability implementation is a connector that serves a capability contract by calling a third-party service over https. It lists the addresses it may call and the credential it needs; the tenant binds it after it passes the contract's tests.",
});
