# ADR-0075 — Markings 有界版与 Integrator 角色（ADR-0069 Ⅰ-I）

状态：接受并实施 · 2026-10-06 · 承接 ADR-0028 D3（字段级读角色）、ADR-0066（角色与范围）、ADR-0070–0074（集成织物）

## 决定

### 1. 标记是随数据走的一个词，不是第二套权限引擎

Connection 与 Dataset 各有一个 `marking`：`internal` < `confidential` < `restricted`（空为未标记）。它只会随数据**抬高**，不会自动降低：

- 数据源经连接拉入数据集 → 数据集的标记抬到连接的标记（`build.dataset.load` 载荷里的 `marking`）；
- 管道运行 → 本次运行的 `marking` = 输入与步骤查表数据集中的最高者，输出数据集抬到它；管道记录 `marking`（只读）为历次最高，血缘面板由此能看到一个页面字段上游最高的标记；
- 降低只能由人编辑数据集或连接。

### 2. 编译到既有机制，而不是新查一次权限

- **机密/受限 → 对象字段必须指明读者**：管道以 confidential/restricted 输入写对象时，若对象有任何字段未设 ADR-0028 D3 的 `read` 角色，发布被拒并列出字段名；运行时输入标记升高后同样拒绝并写进 Last run。这样"谁能看到工资"仍由对象字段的读角色回答，Markings 只保证没有人把机密数据倒进人人可读的字段。
- **受限不出 CSV**：`GET /v1/export/{type}` 对 restricted 的数据集/版本，以及被 restricted 管道写入的对象，一律 POLICY_DENIED。

### 3. Integrator 角色

Builder 应用新增角色 `integrator`：Connections / Data sources / Datasets / Pipelines / Writebacks / Matching rules / Integration health 的全部读写与动作，**不含**对象、页面、流程、函数与发布。构建器左栏给 integrator 一个仅"Integration"分组；页面上原本只放行 builder 的检查改为 builder 或 integrator。接入外部系统的人从此不必是设计本体的人。

### 4. 没做的

- 不做行级/单元格级标记、不做标记继承到链接对象、不做解密/脱敏视图。需要时在对象字段 `read` 角色层面解决。
- 不做"清除级别"的成员属性：机密能否被某角色读，由对象字段读角色说了算，Markings 不另设第二张表。

## 验证

- `capabilities/server/markings_test.go`：confidential 数据集写人人可读的 `salary` 被拒并点名字段；字段设 `read: [hr]` 后发布成功、运行成功、输出数据集继承 confidential；改为 restricted 后数据集版本与对象的 CSV 导出都被拒。
- 手测见 docs/Testing.md「集成织物 Ⅰ-I」。
