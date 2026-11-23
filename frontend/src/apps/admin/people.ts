import { createElement } from "react";
import { PeopleList, PersonPage } from "../../pages/People";
import { elementType } from "../../pages/enterprise";
import { usePath } from "../../router";

function PeopleEntry() {
  const id = usePath().split("/")[3];
  return id ? createElement(PersonPage, { id: decodeURIComponent(id) }) : createElement(PeopleList);
}

export default Object.assign(PeopleEntry, { recordType: elementType });
