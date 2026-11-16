import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.productversion",
  project: true,
  title: "Versions",
  about: "A product version is an immutable package of a product that can be installed and upgraded.",
});
