# 交接：测试与脚本的重刀（ADR-0082）

- **基点 B** = main `55c05a82`（分支 `c883fd7` 与之同树）
- **冻结 HEAD** = `1233d66`
- 路径清单 = `git diff --name-only 55c05a82 1233d66`（326 个文件：322 删除、4 文档/测试修改、1 新 ADR）。全部只在分支侧，按路径取即可；没有生成物变化。

## 删了什么（原则：删掉它会不会让一个长期不变量失去证据？不会就删）

| 处 | 前 → 后 |
|---|---|
| `web/e2e/tests` | 136 → 8 spec（`routes` `business-access` `release-roles` `workflow` `workspaces` `host-sign-out` `integration-fabric` `page-notice`）+ `host.ts` `kit.ts` `workflow-helpers.ts`；`fixtures/`、`workshop-compile.mjs`、`nested-loop-fixture.ts` 删除 |
| `capabilities/server/*_test.go` | 158 → 86 文件；`TestRecordsAtScale`/`TestKnowledgeAtScale` 计时用例删除（`PLATFORM_TIMING` 随之从 release.yml 去掉）；`querySampleRow` 类型搬到 `capability_invocation_test.go` |
| Web 单元测试 | 150 → 37（45 行以下的组件冒烟/导入片段；kernel 与 i18n 全留） |
| 脚本 | `scripts/cleanup-inventory.sh`、`deploy/local/rehearse-lightweight.sh` 删除；README/ADR-0080/Testing.md 引用已改 |

保留族与规则写在 `docs/ADR/0082-tests-that-earn-their-keep.md` 和 `docs/Testing.md`「测试规范」（新功能不自带新测试；e2e 封顶 8 条，加一删一；过程探针不提交）。

## 沙箱里已验证

- `go vet ./...` 与 `go test -count=1 ./...`（capabilities/server 全部包）通过，根包 22 s（之前 76 s，且不再有 `TestRecordsAtScale` 抖动）。
- ui/app/build/kernel/catalog/apps-catalog 单元测试全部通过。

## 请在本地做

1. `make check && make test`，再 `make e2e`（现在就是 8 条；哪条过时修哪条，Testing.md 不再有"遗留路线"一说）。
2. **`apps/*/server`、`solutions/*`、`protocols/*` 的测试我编译不了**（未 vendor）。请用同一原则过一遍：`solutions/hospitality` 13 个测试文件、`manufacturing` 8 个、`apps/erp` 5 个。留 `CheckReplay`/协议契约/授权，删按功能的。
3. `pnpm -r run test` 的包里若有 vitest "No test files found" 报错（某包测试被删空），把该包 `test` 脚本改成 `true`——沙箱里每个包都还剩至少一个，应当不会。
4. 取回后下一轮 B = 你的 main SHA。
