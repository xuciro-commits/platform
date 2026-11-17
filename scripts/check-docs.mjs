#!/usr/bin/env node
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";

const root = new URL("..", import.meta.url).pathname;
const documents = ["README", "intent", "plan", "roadmap", "architecture", "domain", "backend", "frontend", "ux", "design", "glossary"];
const lineBudget = (file) => {
  if (file === "docs/plan.md") return 150;
  if (file === "AGENTS.md") return 120;
  if (file.startsWith(".claude/rules/")) return 40;
  if (file.startsWith(".claude/skills/")) return 150;
  if (file.startsWith(".claude/agents/")) return 60;
  return file.startsWith("docs/") ? 250 : Infinity;
};
const widest = 200;
const problems = [];

for (const name of readdirSync(root + "docs")) {
  if (!documents.some((document) => name === `${document}.md`)) {
    problems.push(`docs/${name}: docs/ holds only the documents listed in docs/README.md. Put content into the document that owns it; adding a document is the owner's decision.`);
  }
}
for (const document of documents) {
  if (!existsSync(`${root}docs/${document}.md`)) problems.push(`docs/${document}.md: missing. It is listed in docs/README.md.`);
}

const inside = (dir, pick) => (existsSync(root + dir) ? readdirSync(root + dir).map((name) => pick(`${dir}/${name}`)).filter((file) => existsSync(root + file)) : []);
const guides = [
  "AGENTS.md",
  "README.md",
  "frontend/AGENTS.md",
  ...inside(".claude/rules", (file) => file),
  ...inside(".claude/agents", (file) => file),
  ...inside(".claude/skills", (file) => `${file}/SKILL.md`),
];
const files = [...documents.map((document) => `docs/${document}.md`).filter((file) => existsSync(root + file)), ...guides.filter((file) => existsSync(root + file))];

for (const file of files) {
  const lines = readFileSync(root + file, "utf8").split("\n");
  const count = lines.at(-1) === "" ? lines.length - 1 : lines.length;
  if (count > lineBudget(file)) {
    problems.push(`${file}: ${count} lines, the budget is ${lineBudget(file)}. Cut what the code already says, what another document already says, and anything that is history or progress.`);
  }
  const document = file.startsWith("docs/");
  if (document && lines[0] === "---") problems.push(`${file}:1: no front matter. A document starts with its title and the question it answers.`);
  let fenced = false;
  lines.forEach((text, index) => {
    const at = `${file}:${index + 1}`;
    if (text.startsWith("```")) fenced = !fenced;
    if (fenced) return;
    if (document && [...text].length > widest) {
      problems.push(`${at}: ${[...text].length} characters on one line, the limit is ${widest}. One line says one thing: split it into short lines or cut it.`);
    }
    if (document && /^#+ .*(差距|变更记录|更新日志|进度日志|已完成)/.test(text)) {
      problems.push(`${at}: unfinished work belongs in docs/plan.md or docs/roadmap.md; finished rows are deleted and history is in git. Remove this section.`);
    }
    for (const match of text.matchAll(/\]\(([^)\s#]+)(?:#[^)]*)?\)/g)) {
      const target = match[1];
      if (/^[a-z]+:/.test(target)) continue;
      if (!existsSync(join(root, dirname(file), target))) problems.push(`${at}: link to "${target}" does not resolve. Point it at a file that exists.`);
    }
    for (const match of text.replace(/\]\([^)]*\)/g, "]").matchAll(/(?<![\w/.<>-])docs\/[\w./-]*[\w/-]/g)) {
      if (!existsSync(root + match[0]) && !existsSync(`${root}${match[0]}.md`)) {
        problems.push(`${at}: "${match[0]}" does not exist. docs/README.md lists the documents; name one of them.`);
      }
    }
  });
}

if (problems.length > 0) {
  console.error(problems.join("\n"));
  console.error(`\ndocs: ${problems.length} problem(s). Fix each one as its message says; do not edit scripts/check-docs.mjs to make it pass.`);
  process.exit(1);
}
console.log(`docs: ${documents.length} documents and ${guides.length} guidance files within budget, every reference resolves`);
