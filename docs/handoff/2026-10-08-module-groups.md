# 交接：模块配置→发布审查收口（WorkQueue 第一项 1/3）

- **基点 B** = main `5cd6824d`（分支 `44dda7f` 同树）
- **冻结 HEAD** = `2269854`
- 路径：`git diff --name-only 5cd6824d 2269854` → 5 个文件，全在 `web/packages/build/src`：`releases/diagnostics.ts`（新）、`releases/release.tsx`、`workshop/ModuleWorkbench.tsx`、`index.tsx`、`i18n.ts`。

## 做了什么

1. **编辑器提前指出**：`ModuleWorkbench` 的 Problems 底栏现在镜像宿主 `CheckGroups` 的四条拒绝（分组无标题 / 分组无页面 / 分组列出模块不含的页面 / 页面在两个分组下），每条 `locate` 跳到 Navigation 检查器；状态条的问题计数随之变化。宿主校验不变，不自动猜页面归属。
2. **发布审查给中文原因和返回入口**：`releases/diagnostics.ts` 识别宿主的已知拒绝句（分组四条、`application X: it holds no page`、`no published resource`、`application header …`），用 `t()` 翻译并给出"打开模块导航 / 打开页眉"按钮（`open({view:"module", params:{id, application, focus}})`）；未识别的句子原样显示，识别的在下方附原文 `code`。`module` 视图新增 `focus` 参数。
3. 作用域：审查从模块打开时用 `useApplicationScope()`；直接选 `kind=app` 时用其 id。

## 请本地核对（WMS 复现）

- 在 WMS 模块加一个空分组"仓库管理"：Problems 出现该条，点它跳到 Navigation；不修直接 Publish → 审查显示中文原因 + "打开模块导航"按钮，点回模块、把页面勾进分组或删掉分组、再发布通过。
- `node scripts/catalog.mjs check` 在沙箱对 main 本身就报 stale（你的 `make check` 过了，应是环境差异），我**没有**提交重生成的 json；本地若确有变化请重生成。

## 第 2/3 步（两行业连贯路线）需要浏览器，留给你

路线建议（每行业一条，不新建机制）：
- 酒店 8495：`seed-hospitality.sh` → 对象（在 `crm.opportunity` 之上或自定义对象）加一个动作 → 页面 → 联合候选 → 发布 → `desk` 操作并故意触发一次动作条件拒绝 → 在 Runs/Problems 定位原因。
- 制造 8490：数据源/数据集/管道（integration-fabric 的 UI 入口）→ 对象/流程 → 页面 → 候选 → `hotel-a → hotel-test` 式晋级到 plant 的测试租户 → 普通成员操作 → 失败定位。

停止条件按负责人原话：路线可完成、拒绝可理解且可定位。每个真实断点回我"路径 + 现象 + 期望"，我修；不是断点的不动。
