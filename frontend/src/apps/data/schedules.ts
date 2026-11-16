import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.schedule",
  project: true,
  title: "Schedules",
  about: "A schedule is a time plan that runs pipeline builds.",
});
