#!/usr/bin/env node
import { readFileSync } from "node:fs";

const file = process.argv[2];
if (!file) {
  console.error("commit-msg: 缺少提交信息文件路径");
  process.exit(1);
}

const raw = readFileSync(file, "utf8")
  .split(/\r?\n/)
  .filter((line) => !line.startsWith("#"));

while (raw.length > 0 && raw[0].trim() === "") raw.shift();
while (raw.length > 0 && raw.at(-1).trim() === "") raw.pop();

const subject = raw[0] ?? "";
if (/^(Merge |Revert |fixup! |squash! )/.test(subject)) process.exit(0);

const types = ["功能", "修复", "优化", "性能", "重构", "文档", "验收", "清理", "合并"];
const errors = [];

const isMerge = /^合并: .+/.test(subject);
const subjectPattern = new RegExp(`^(${types.join("|")}): [^\\s—][^—]*——.+$`);

if (!isMerge && !subjectPattern.test(subject)) {
  errors.push(`标题格式须为「类型: 范围——行为变化」（允许类型：${types.join(" / ")}，使用英文冒号加空格与中文破折号 ——）`);
}
if (!/[\u4e00-\u9fff]/.test(subject)) {
  errors.push("标题须使用中文描述行为变化");
}

if (!isMerge) {
  if (raw.length < 3 || raw[1].trim() !== "") {
    errors.push("正文须与标题空一行，并按「范围与行为变化」「检查」等小节分组");
  } else {
    const body = raw.slice(2).filter((line) => !/^[A-Za-z-]+:\s/.test(line));
    const sections = new Map();
    let current = null;
    for (const line of body) {
      const trimmed = line.trim();
      if (/^(范围与行为变化|替代与删除|重构与新增|文档|性能对比|检查)$/.test(trimmed)) {
        current = trimmed;
        if (!sections.has(current)) sections.set(current, []);
        continue;
      }
      if (current && /^-\s+\S/.test(trimmed)) {
        sections.get(current).push(trimmed);
      }
    }
    for (const required of ["范围与行为变化", "检查"]) {
      if (!sections.has(required) || sections.get(required).length === 0) {
        errors.push(`正文缺少「${required}」小节或小节下缺少 "- " 条目`);
      }
    }
  }
}

if (errors.length > 0) {
  console.error("提交信息未通过规范检查：");
  for (const err of errors) console.error(`- ${err}`);
  console.error(`
标准格式示例：
性能: 后端热路径——缓存 Build.Manifest 并消除 ForkRead 深拷贝

范围与行为变化
- 为 Build.Manifest 缓存内建静态实体与页面，避免单次提交重复重建。
- 为 Catalog 增加 CloneActions，ChangeLog.Fork 改为不可变父快照链。

检查
- make check：通过。
- go test -race ./host：相关并发用例通过。`);
  process.exit(1);
}
