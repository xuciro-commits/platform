# ship-3 客户开通：导入与邀请

车道：ship    依赖：无；规模与故障部分等 kit-4
能改：frontend/src/pages/ImportWizard.tsx、frontend/src/apps/admin/**、frontend/src/pages/** 里导入与成员相关的页面、frontend/e2e/crm.spec.ts、scripts/frontend-fixtures/** 里导入用的工作簿
不碰：capabilities/server/**（后端归 kit-4，缺接口先在看板问 kit）；frontend/e2e/official.spec.ts、marketplace.spec.ts（等 R5 与 kit-1 落地）

## 要做到
- 做 roadmap 3.4b 的界面一半：一个工作簿里的线索、客户、联系人一次导入，按列标题映射，联系人与客户的引用落对。
- 混入的无效行在向导里标出原因，就地改了重试，其余行不受影响；导入进行中，同租户另一页能查看、编辑。
- 管理员按客户账号邀请同事并授 CRM 角色；同事登录后看得到导入的线索，按角色能改或只读。
- trunk：crm 的 “import” 用例扩到三类对象，另加一条 invite 用例；`make trunk SPEC=crm` 与 `make trunk-pg SPEC=crm` 通过。浏览器跑不了就照 tasks/README.md 交出验收。

## 交接
- 无
