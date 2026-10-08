# ADR-0082 只留长期不变量：测试与脚本的重刀

状态：已接受（2026-10-08）。

## 背景

本地集成代理一次完整验证要半个多小时；e2e 136 个 spec（1.36 MB，多数是单行压缩、按部件一个）、宿主根包 158 个测试文件 28k 行、Web 150 个单元测试、十几个脚本。其中大半是"做了一个功能顺手写的过程测试"：它证明当时那块代码按当时的想法工作，之后再没变过，却在每次验证里跑一遍。这与 ADR-0080 的方向相反。

## 决定

只有一条原则：**它为什么存在？删掉它会不会让一个长期不变量失去证据？** 不能回答的删掉。长期不变量是：契约（API/类型/翻译）、原子提交与崩溃恢复、重放兼容、租户隔离与授权、发布生命周期、身份与会话、外部契约（OIDC/MCP/A2A/协议）。按部件、按功能、按一次 ADR 的"冻结-激活-重放"复制品，不是。

| 处 | 之前 | 之后 | 删了什么 |
|---|---|---|---|
| `web/e2e/tests` | 136 spec | 8 spec + kit | 全部 `page-*`（除 kit 样板 `page-notice`）、`application-*`、`link-*`、function/model/catalog/record-work 等按功能路线；`fixtures/`、`workshop-compile.mjs`、`nested-loop-fixture.ts` |
| `capabilities/server/*_test.go` | 158 文件 | 86 文件 | 全部 `page_*`、`application_*_release`、`decimal_*`、`property_*`、按部件聚合、`build_{action_rules,conditions,creates,inventory}`、`process_{candidate,function}`、`typed_process`、`simulate_{candidate,function}`、`release_{binding,candidate}`、`testplan`、`work`、`ops`、`meaning`、`chart_marks`、`pivot`、`projection`、`files_regions`、`flow_query_version`、`function_{conversation,evaluation_input}`、`navigation_release`、`capability_page`、`link_{archive,cardinality}`、`pipelines_enterprise`；`TestRecordsAtScale`/`TestKnowledgeAtScale` 两条计时用例（抖动来源） |
| Web 单元测试 | 150 | 37 | 45 行以下的组件渲染冒烟与导入片段测试（kernel 与 i18n 除外） |
| 脚本 | `scripts/` 9 + `deploy/local/` 6 | 7 + 4 | `cleanup-inventory.sh`（一行 shell 即可）、`rehearse-lightweight.sh`（`lightweight_test.go` 已证明） |

保留的宿主测试族：`accepted_*`（原子提交/崩溃）、`quarantine`/`deploy_recovery`/`journal_*`/`lightweight`/`oidc`/`deploy_admin`（持久与身份）、`tenancy_*`/`tenants`/`upgrade_authorization`/`submit_tenant`/`build_access`（隔离与授权）、`environment_*`/`release_install`/`direct_install`/`joint_draft`（生命周期）、`api`/`languages`/`definitions`/`host`（契约）、`console_compat`/`core_seed_replay`/`legacy_agent_context`（重放兼容）、`build`/`build_actions`/`build_approval`/`build_function`/`build_query`/`process`/`flow`/`function`/`agent*`/`ai`/`review_ai`（租户定义能力的核心行为）、外部契约与织物（`effects`/`writebacks`/`requests`/`sources_profiles`/`mcp`/`a2a`/`mail`/`io`/`files`）。

## 规则（并入 docs/Testing.md）

- 新功能不自带新测试，除非它改变了上面某个不变量；改了不变量，改那条已有测试。
- 浏览器路线封顶：8 条。要加一条，先删一条。
- 过程中的探针写在本地，不提交。

## 后果

`make test` 宿主根包约 60 秒（2 核沙箱），`make e2e` 8 条；`make verify` 不再是半小时。删掉的测试在 Git 历史里，需要重现某个旧场景时按文件名找回，而不是常驻。
