# kit-7 升级时扩展不兼容就整批拒

车道：kit    依赖：kit-6 之后
能改：capabilities/server/services/devops/packages/**、capabilities/server/host/release_*.go、package_installation.go 及对应测试；
  frontend/src/host/gen/（只经 make contract 生成）
不碰：frontend/src/apps/delivery/**、frontend/e2e/marketplace.spec.ts（界面与浏览器用例归 studio-4）；持久化格式与格式纪元

## 要做到
- 做 roadmap 3.3c 的服务端。先核实现在扩展不兼容时升级会不会被拒：91d037a21 只把扩展与版本约束列进了审阅。
- 两种不兼容：扩展挂的目标对象在新版里被删；扩展声明的版本范围不含新版。
  升级提交时服务端指名拒绝（哪个扩展、哪条原因），整批不落，不做一半；审阅数据带同样的原因，界面照着显示即可。
- 兼容的升级照常通过，扩展的字段、动作与数据都在（3.3b 的服务端一半）。
- 内核不变量用例：两种不兼容各一条拒绝、一条兼容通过、拒绝后租户照常可写、重放一致。
- land 时在看板告诉 studio 审阅里拒绝原因的字段名，studio-4 照它做界面。

## 交接
- 无
