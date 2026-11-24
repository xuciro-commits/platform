import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.connectorsetup",
  title: "Connector setups",
  about: "A connector setup gives one capability implementation this workspace's address and credential and runs the contract's tests against it. A connector can be enabled for its capability only after its tests pass with the current setup.",
});
