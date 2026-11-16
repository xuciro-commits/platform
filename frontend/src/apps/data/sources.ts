import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.source",
  project: true,
  title: "Sources",
  about: "A source pulls rows from an external system into records of one object type, on a schedule or on request.",
});
