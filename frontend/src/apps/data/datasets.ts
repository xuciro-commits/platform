import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.dataset",
  project: true,
  title: "Datasets",
  about: "A dataset holds rows as they came from a source or a pipeline, one version per load.",
});
