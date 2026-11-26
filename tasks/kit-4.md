# kit-4 客户规模的批量导入

车道：kit    依赖：无（kit-2 之后做）；ship-3 的规模与故障部分用它
能改：capabilities/server/services/objects/**、capabilities/server/kernel/store/** 的批量提交与测试、scripts/load-*、Makefile 的 load 目标
不碰：frontend/**（界面归 ship-3）；格式纪元与接受映像（B 级，先问 lead）；内核语义按 tasks/README.md 的“内核”走

## 要做到
- 做 roadmap 3.4b 的后端一半：逐行授权与校验、同事务批量提交、两类故障注入、原键重试、重放一致。
- 混入一条无效记录时其余行入账，各行保留独立的幂等键与结果；`make test` 与 `make test-pg` 各有用例。
- 明确回滚、提交结果未知两类故障各一条用例：整批可重试、租户仍可写、无丢失无重复、序号连续。
- 批量接受 1,000 条后，内存、文件、快照加尾部与 PostgreSQL 四种重放结果一致（K1）。
- `make load-sql` 的导入模式跑一次接近客户规模的数据，导入期间同租户的读与编辑不报错；数字写进提交信息。
- 导入接口有变化时在看板给 ship 发 `--kind contract`。

## 交接
- 无
