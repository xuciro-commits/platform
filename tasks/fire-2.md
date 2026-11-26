# fire-2 升级审阅要知道哪些扩展挂在包上

车道：fire    依赖：fire-1 之后做；kit-1 的 3.3a、3.3c 用它
能改：services/devops/**、services/ontology/**；碰到内核按 tasks/README.md 的“内核”走（A 级）
不碰：frontend/**（界面归 kit 与 studio）

## 要做到
- 升级审阅能拿到：哪些附属对象的 `extends` 指向这个包的类型、它们所在的项目与版本范围。
- 新版删了目标类型、或版本范围不含新版时，审阅结果把这个扩展列为不兼容，并写明原因。
- 只读，不改已有行为；`make test` 加一条用例：一个扩展兼容、一个不兼容。
- 动手前在看板给 kit 发接口草图（`say --to kit --kind contract`），kit 可以先用假实现做界面。

## 交接
- 无
