# Tasks

## 1. 官方集成与模块提炼

- [x] 1.1 初始化官方 Codex Skill，固定根 Makefile CLI 版本；`list --json` 确认当前仓库根，配置声明模块规则。
- [x] 1.2 从旧设计与当前源码提炼十二个模块的规范差量；逐份确认主责、历史责任、具体场景与源码定位，不新增运行行为。

## 2. 现行规范与入口

- [x] 2.1 按 sync Skill 将十二份差量合成现行模块规范，确认 `list --specs` 能识别完整层级路径，主规范无差量标题。
- [x] 2.2 更新 AGENTS、README、Architecture 和 WorkQueue 的职责指针；规范目录说明主责、官方 Skill 用法和单一项目顺序。

## 3. 集中一致性检查

- [x] 3.1 运行 `make check-specs`、本次文档链接及来源检查与 `git diff --check`；确认只改变规范、工具入口和文档，不报告未运行的业务测试。

## Workflow follow-up

- 主规范同步且必要检查完成后，按 archive Skill 归档本次文档基线；确认活动变更为空、归档任务完成。
