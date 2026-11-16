import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.listener",
  project: true,
  title: "Listeners",
  about: "A listener is an inbound webhook that delivers payloads to a source.",
});
