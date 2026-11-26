# lead-2 冻结格式纪元

车道：lead    依赖：无；在 3.4 开通客户之前做完
能改：kernel 的格式纪元与迁移、scripts/check-*、docs/**
不碰：别的车道正在做的任务文件

## 要做到
- 做 roadmap 3.4a：`FormatEpoch` 之后的格式变化只经版本化迁移。
- `make check` 加一条核对：改纪元必须同时带迁移与用例，缺一项就指名拒。
- 这是 B 级内核：先 `board lock kernel`，做完 `unlock`。

## 交接
- 无
