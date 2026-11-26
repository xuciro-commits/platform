# fire-3 PostgreSQL 上重启后快照恢复失败，租户被隔离

车道：fire    依赖：无；修复要动内核时先和 kit 对齐（kit 正锁着 kernel 做 kit-6）
能改：capabilities/server/kernel/snapshot*.go、capabilities/server/host/quarantine.go、对象页编码所在文件、host 的 PostgreSQL 测试
不碰：frontend/**；kit-6 的批导入与监听器文件；`FormatEpoch` 与持久化格式（要改先停下问 lead）

## 要做到
- 复现：kit 工作树 `make load-sql`（导入模式，种子 1000 条）后重启宿主，`.build/kit/.build/load/kit-4/server.log` 里
  `restore snapshot 1012 ... build: page loadproject.loaditem: ERROR_CODE_INVALID_ARGUMENT`，租户被隔离；这时导入还没开始。
- 先只读查清保存与恢复两侧对这个对象页的编码哪里不一致；嫌疑 1b5df0dcb（SQL 便携快照）、37699a228（停机快照）、48580d57f（备份导入导出）。
  结论 `say --to lead`，写明修法要不要动格式。
- 动内核前 `node scripts/board.mjs lock kernel`；kit 还拿着就 `say --to kit` 约好先后，不同时改同一批文件。
- 同一场景重启后不再隔离，记录、修订、编号与重启前一字不差（做 roadmap 3.1c、3.1d 的 PostgreSQL 一半）。
- `make test-pg` 加一条复现它的用例：修前红、修后绿。合入前跑 `make check`、`make test`、`make test-pg`、`make trunk-pg`。

## 交接
- 无
