# ADR-0127 — XXL：构建定义、接受与发布生命周期收敛

状态：接受执行（2026-10-17），归 ADR-0125 XXXL。保存的发布/接受字节及公开 schema 不变。

## 现状问题

Build 的对象、页面、应用、函数、流程、协议定义等由不同 owner 产生，但接受发布、校验、安装、重放和恢复在 `apps/build/build.go`、`asset_lifecycle.go`、`integration_release.go`、`definition_history.go` 及 host callbacks 间分布。validate/apply 必须分开承担“可否接受”和“从已接受字节重建”两种责任；其余重复解码、重复分派和重复身份校验不应因此各自演进。

## 决定

- `apps/build` 是 Build 资产字节到 Build 声明的唯一解释者；宿主是安装效果及运行目录的 owner；持久接受日志是恢复的输入权威。
- 对同一 `(schema, image)`，校验和应用必须使用相同的解码/身份验证规则。可共享不可变的解析结果，但不能缓存跨请求/版本的可变状态或绕过 restore 再验证。
- schema 列表、image decoder、validator、installer 不再分别维护彼此漂移的 switch/名单。只有在不牺牲静态穷尽性时才允许单一 dispatch；优先以 Go 编译期穷尽分支，不引入通用插件框架。
- 发布的原始字节、版本 ID、引用身份、迁移输入和历史读者不重写。被替代的普通发布路径必须同批删除。

## 施工顺序

1. 列出全部 `AcceptedPublicationSchemas()` 的 schema、image 来源、唯一解释函数、校验、应用/安装、历史恢复调用点。
2. 对重复 `json.Unmarshal`/`wasPublished` 检查建立同一纯解析结果路径，并让 Validate 和 Apply 使用它；如果接口要求跨调用不共享状态，则两个调用仍各自解析，但同一 decoder/校验函数只实现一次。
3. 删除已被共同路径替代的 per-type 解码、身份检查与分散旧分支，迁移应用包/流程/集成/版本历史消费者。
4. 生成一个矩阵型测试，逐 schema 验证有效、缺字段、身份不匹配、版本历史、恢复与幂等安装；不新增一套对照测试框架。

## 首批实现

`publicationImage` 原来返回五个并列值，`ValidateAcceptedPublication` 与 `ApplyAcceptedPublication` 又分别重新解码对象并提取已发布定义。现在它返回一个局部 `acceptedPublication` 值，包含实体、动作、页面、应用与已解码对象；Validate 和 Apply 使用同一 identity-checked object，不再重复 unmarshal/`wasPublished` 检查。外部 `AcceptedPublisher` 接口、schema、持久字节和各类特殊 validator/installer 均未改。此切片仅覆盖对象/页面/应用的共同部分，不代表全部资产 dispatch 已收敛。

## 保证与退出门

保留 accepted bytes 不变、结果恢复不重新运行用户动作、不重复外部效果、不可变定义历史、租户授权和被引用资产所有权。对恢复失败维持确定拒绝/隔离路径。只有相关 schema 的接受、日志恢复、旧版本安装及依赖引用测试通过后，才能删除旧分支。

完成度用删除/合并的 handwritten production code 与剩余 dispatch owner 衡量；单纯把 switch 移到 registry 或把大文件拆开不算完成。