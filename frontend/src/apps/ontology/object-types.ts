import { recordList } from "../../pages/RecordList";

export default recordList({
  type: "build.object",
  project: true,
  title: "Object types",
  about: "An object type describes one kind of business object: its properties, links and actions.",
  fields: ["title", "name", "plural", "state"],
  instances: { field: "installed", path: "/ontology/objects" },
});
