import { readFileSync } from "node:fs";

const catalogUrl = new URL("../frontend/public/avatars/catalog.json", import.meta.url);
const catalog = JSON.parse(readFileSync(catalogUrl, "utf8"));
const byId = new Map(catalog.items.map((item) => [item.id, item]));

function pick(id) {
  const item = byId.get(id);
  if (!item) throw new Error(`demo-roster: unknown avatar id ${id}`);
  return item;
}

export const seatProfiles = {
  root: {
    displayName: "系统管理员",
    title: "平台系统管理员",
    email: "root@acme.example",
    avatar: pick("gl-014").ref,
  },
  "admin-1": {
    displayName: "王丹",
    title: "运营与平台负责人",
    email: "wangdan@acme.example",
    avatar: pick("cn-010").ref,
  },
  "builder-1": {
    displayName: "韩奥",
    title: "解决方案架构师",
    email: "hanao@acme.example",
    avatar: pick("cn-007").ref,
  },
  "user-1": {
    displayName: "田雪",
    title: "华南区客户经理",
    email: "tianxue@acme.example",
    avatar: pick("cn-006").ref,
  },
  "manager-1": {
    displayName: "赵敏",
    title: "销售总监",
    email: "zhaomin@acme.example",
    avatar: pick("cn-008").ref,
  },
  "publisher-1": {
    displayName: "林致远",
    title: "产品包发布经理",
    email: "linzhiyuan@acme.example",
    avatar: pick("cn-009").ref,
  },
  "rep-1": {
    displayName: "程浩然",
    title: "大客户销售代表",
    email: "chenghaoran@acme.example",
    avatar: pick("cn-023").ref,
  },
  "rep-2": {
    displayName: "秦梦瑶",
    title: "行业销售代表",
    email: "qinmengyao@acme.example",
    avatar: pick("cn-024").ref,
  },
};

export function resolveMemberProfile(id, entry) {
  const base = seatProfiles[id] ?? {};
  if (typeof entry === "string") {
    return { ...base, displayName: entry };
  }
  return { ...base, ...(entry ?? {}) };
}

export function enterpriseRosterSteps() {
  const linked = [
    { id: "person-admin-1", member: "admin-1", name: seatProfiles["admin-1"].displayName, avatar: seatProfiles["admin-1"].avatar, kind: "executive" },
    { id: "person-builder-1", member: "builder-1", name: seatProfiles["builder-1"].displayName, avatar: seatProfiles["builder-1"].avatar, kind: "architect" },
    { id: "person-user-1", member: "user-1", name: seatProfiles["user-1"].displayName, avatar: seatProfiles["user-1"].avatar, kind: "account-manager" },
    { id: "person-manager-1", member: "manager-1", name: seatProfiles["manager-1"].displayName, avatar: seatProfiles["manager-1"].avatar, kind: "director", unit: "sales", role: "chair" },
  ];

  const staff = [
    { id: "person-cn-005", avatarId: "cn-005", unit: "sales-north", kind: "account-manager", role: "employee" },
    { id: "person-cn-009", avatarId: "cn-009", unit: "acme", kind: "vp", role: "chair" },
    { id: "person-cn-012", avatarId: "cn-012", unit: "operations", kind: "operations-lead", role: "chair" },
    { id: "person-cn-023", avatarId: "cn-023", unit: "sales-north", kind: "sales-rep", role: "employee" },
    { id: "person-cn-024", avatarId: "cn-024", unit: "sales-south", kind: "sales-rep", role: "employee" },
    { id: "person-cn-026", avatarId: "cn-026", unit: "operations", kind: "delivery-manager", role: "employee" },
    { id: "person-cn-041", avatarId: "cn-041", unit: "sales-south", kind: "solutions-engineer", role: "employee" },
    { id: "person-cn-044", avatarId: "cn-044", unit: "operations", kind: "customer-success", role: "employee" },
    { id: "person-gl-001", avatarId: "gl-001", unit: "sales", kind: "global-partner", role: "employee" },
    { id: "person-gl-014", avatarId: "gl-014", unit: "acme", kind: "advisor", role: "employee" },
  ];

  const steps = [];
  for (const person of linked) {
    steps.push([
      "enterprise.element.add",
      "enterprise.element",
      person.id,
      { stereotype: "ActualPerson", name: person.name, kind: person.kind, avatar: person.avatar },
    ]);
    steps.push([
      "enterprise.relationship.add",
      "enterprise.relationship",
      `signin-${person.member}`,
      { stereotype: "SignsInAs", source: `member:${person.member}`, target: person.id },
    ]);
    if (person.unit) {
      steps.push([
        "enterprise.relationship.add",
        "enterprise.relationship",
        `role-${person.id}-in-${person.unit}`,
        { stereotype: "ActualOrganizationRole", source: person.id, target: person.unit, role: person.role },
      ]);
    }
  }

  for (const member of staff) {
    const entry = pick(member.avatarId);
    steps.push([
      "enterprise.element.add",
      "enterprise.element",
      member.id,
      { stereotype: "ActualPerson", name: entry.name, kind: member.kind, avatar: entry.ref },
    ]);
    steps.push([
      "enterprise.relationship.add",
      "enterprise.relationship",
      `role-${member.id}-in-${member.unit}`,
      { stereotype: "ActualOrganizationRole", source: member.id, target: member.unit, role: member.role },
    ]);
  }

  return steps;
}
