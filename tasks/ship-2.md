# ship-2 备份与恢复

车道：ship    依赖：ship-1
能改：capabilities/server/cmd/**、capabilities/server/kernel/store/**（只加导出与导入，不改格式）、frontend/e2e/official.spec.ts、Makefile
不碰：格式纪元与接受映像（B 级，先问 lead）

## 要做到
- 做 roadmap 3.1d。
- 导出 official → 新库导入 → 新宿主起来后 `make trunk-pg SPEC=official` 照跑通过。

## 交接
- 无
