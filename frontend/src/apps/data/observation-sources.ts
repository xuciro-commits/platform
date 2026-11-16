import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.observation",
  project: true,
  title: "Observation sources",
  about: "An observation source reads summaries of external time series and applies the declared decisions.",
});
