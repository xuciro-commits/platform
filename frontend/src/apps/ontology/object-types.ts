import { ObjectCreator } from "../../pages/ExtensionCreator";
import { ontologyCatalog } from "../../pages/OntologyCatalog";

export default ontologyCatalog({ kind: "object", title: "Object types", about: "An object type describes one kind of business object: its properties, links and actions.", author: {
  type: "build.object",
  project: true,
  title: "Object types",
  about: "An object type describes one kind of business object: its properties, links and actions.",
  fields: ["title", "name", "namespace", "state"],
  headers: { title: "Label", name: "API name", plural: "Plural" },
  instances: { field: "installed", path: "/ontology/objects" },
  creator: ObjectCreator,
} });
