# kit-3 连接器产品化：外接供应商能用，trunk 走通

车道：kit    依赖：无
能改：capabilities/server/services/actions/**、capabilities/server/services/resources/authoring/build_connector_setup.go、
  build_conformance.go、build_capability_call.go 及同目录的测试、frontend/src/pages/Capabilities.tsx、
  frontend/src/pages/ConnectorSetupCard.tsx、frontend/src/apps/ontology/connector-setups.ts、frontend/e2e/connector.spec.ts、
  deploy/packages/** 里新加的测试契约包、scripts/frontend-fixtures/ 里新加的连接器夹具、Makefile 里 trunk 起测试桩的部分
不碰：kernel/**（含 kernel/protocol，要改先按 README 的“内核”走）；防内网拨号的默认规则；
  已交给 fire 的五个路径（official.spec.ts、marketplace.spec.ts、crm-official-0.3.1/0.3.2.json、party-0.1.0.json）

## 要做到
- 补上外接云供应商还缺的三件（roadmap B3 “之后”末条）：请求体按模板组装、Basic 鉴权、请求签名（HMAC-SHA256）。
  密钥与密码只走已有凭据，不进日志、调用记录与页面；签名有固定向量的单元用例。
- 管理员在“管理 → 能力”配置连接器（地址、凭据、模板），跑一致性测试，每条看到通过或失败的原因，全过后才能启用。
- 一次真调用：记录页的动作经连接器出站，答复写回记录；结果不明的调用在“能力调用”页可见，由人重试或标失败，不自动重发。
- 测试桩：trunk 起本机 HTTPS 桩（自签证书只给测试宿主信任）。放行本机只在 trunk 宿主的测试开关下生效，默认仍拒内网；
  开关若要改宿主启动参数或内核，先在看板问 lead。
- K45、K46 继续通过。
- trunk：`make trunk SPEC=connector` 走通：配置 → 错凭据测试失败 → 改对全过 → 启用 → 调用写回 → 桩超时记为结果不明 → 人处置；
  `make trunk-pg SPEC=connector` 也过。land 时在看板告诉 lead 功能矩阵“能力：契约、绑定、连接器”一行该怎么改。

## 交接
- 无
