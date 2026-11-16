import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.writeback",
  project: true,
  title: "Writebacks",
  about: "A writeback sends an accepted action to an external system through a connection, in order and with retries.",
});
