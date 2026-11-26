# kit-2 party 的结构化地址

车道：kit    依赖：无；改 marketplace.spec.ts 与夹具等 kit-1 落地，浏览器那一步照 tasks/README.md 交出
能改：官方包库的 party 包、deploy/packages/**、scripts/frontend-fixtures/**、scripts/frontend-seed.json、frontend/e2e/marketplace.spec.ts、capabilities/server/services/devops/releases/**（只为 R7：升级可新增可选引用字段）
不碰：kernel/**、其余 services/**（缺平台能力先留言给 lead）

## 要做到
- 做 roadmap 2.9 剩下的一项：party 的地址改为可选的结构化引用（国家、区划、街道），引用 geo。
- party 发下一版，已有的文本地址升级后还在；`make packages` 通过。
- trunk 的夹具换成新版 party；marketplace 的 publisher 用例加一步：给单位填国家与区划，记录页读出名称。

## 交接
- 无
