# kit-5 升级审阅要知道哪些扩展挂在包上

车道：kit    依赖：kit-2 之后、kit-4 接入 host 之前做；studio-4 用它
能改：capabilities/server/services/devops/**、capabilities/server/services/ontology/**；碰到内核按 tasks/README.md 的“内核”走（A 级）
不碰：frontend/**（界面归 studio-4）

## 要做到
- 升级审阅能拿到：哪些附属对象的 `extends` 指向这个包的类型、它们所在的项目与版本范围。
- 新版删了目标类型、或版本范围不含新版时，审阅结果把这个扩展列为不兼容，并写明原因。
- 只读，不改已有行为；`make test` 加一条用例：一个扩展兼容、一个不兼容。
- 动手前在看板给 studio 发接口草图（`say --to studio --kind contract`），studio 可以先用假实现做 studio-4 的界面。

## 交接
- 原为 fire-2，fire 未开工；按新分工（偏后端的活给 kit）移到 kit。
