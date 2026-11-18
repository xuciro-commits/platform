import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "agent.evaluation",
  title: "Evaluations",
  about: "An evaluation runs an agent over declared cases against a saved release candidate and keeps the report.",
});
