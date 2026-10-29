# Tasks

负责人已授权直接实施。Web视觉清单、同源安全声明生成、格式3源码/可视化适配、声明文字本地化与原WASM表单预览已编码；五要素Go契约与旧路径删除正在同批收口，跨层验收尚未运行；项目顺序归 `docs/WorkQueue.md`。逐文件责任清单及迁移条件见 `design.md` 附表 A–F；物理路径按战役1 `file-consolidation.md` 中原精确路径的唯一行解析，不能把本change中的旧owner定位重建为新普通路径。

## 1. 紧凑安全契约与旧原件

- [ ] 1.1 在原 `platform/page_model.go`、`page_queries.go`、`page_rules.go` 定义格式3的五要素与有界原件表示，接通原Build草稿和生成API；用既有owner测试核对类型、动作签名、输入输出、省略/空动作集合及非法绑定拒绝，API类型按原工具生成。
- [ ] 1.2 将旧无document及v2安全字段/profile门槛冻结为 `platform/page_compat.json`，在原页面decoder纯投影到同一安全契约；把原历史/profile/零值/显式指针/作用域用例合并到 `page_model_test.go`、`page_queries_test.go`，核对全部受支持profile与原内容摘要不变，未知格式及不可无损映射明确拒绝。
- [ ] 1.3 改接原安装、依赖闭包、获权投影、模型/查询/导航/多模型/Compute读取owner，不从WidgetSpec构造执行依赖；原owner测试覆盖跨scope、撤权、来源字段、确切版本、固定查询、完整性及展开预算，确认文件/AI/Operation/Work仍走原权限与接受入口。
- [ ] 1.4 将原内容摘要、retained页面、包与候选引用切到原件身份，保留原旧输入和封存结果的不同恢复责任；用已有冻结/安装/恢复owner用例核对同包同版内容身份、历史页面/嵌入与在途版本，不重写旧字节或重新求值已接受业务；同步Architecture中的页面安全/呈现归属。

## 2. 视觉清单迁回Web并删除旧页面文件

- [ ] 2.1 将原77项清单迁为 `web/packages/app/src/pageui/widgets.json`，从同一源推导视觉类型及验证；原运行注册、作者注册、导入和源码/可视化编辑消费同一契约，原注册与属性检查测试证明不存在第二份schema、缺失Renderer或Inspector，也不通过提高组件版本排除旧配置。
- [x] 2.2 按附表E移除35个UI文件对Go页面视觉类型/清单的依赖，保留UI原primitive props、文件/字节/URL/GLB与资源释放检查；kernel视觉authoring类型迁app，由原app适配器传入有界binding数据；运行原UI/App/Build受影响测试及类型/边界检查，确认没有 `ui → app` 反向依赖。
- [ ] 2.3 将附表A的布局、尺寸、输入、图表、卡片、空间与协作视觉检查迁入Web原owner，安全字段只保留在五要素和原资源owner；将附表B中的纯视觉用例迁到前端，保留安全与历史情景；验证全部现有注册可编辑和呈现，未知配置原值保留，正常运行器不能执行raw中伪造的请求。
- [ ] 2.4 完成前述迁移后删除附表A的75个旧生产文件及附表B的83个旧测试路径；保留3个生产/2个测试页面文件，清除Go视觉分派、embed清单及专用视觉类型，按生成器更新API/Catalog和引用；用文件库存、真实依赖图、边界检查和 `git diff --check` 核实，无仅改名的旧执行路径或同语义副本。

- [ ] 2.5 在原获权页面/发现响应投影原translator已经解决的已发布owner词典，Web原呈现owner用原t只翻WidgetSpec清单声明文本，宿主翻译器跳过不透明字节且只翻typed元数据；原本地化测试覆盖英文原件/业务值不变、草稿不影响运行、租户覆写及owner冲突沿同一结果、世代失效，不新增语言服务、词典存储或开放管理员词汇接口。

## 3. 同源Formula和浏览器联动

- [ ] 3.1 按战役1总清单同一原路径行，将 `internal/construction/formula.go` 的纯parser/lowering归入 `capabilities/server/internal/rules/formula_lowering.go`，必要声明/适配与当前 `apps/build/formula.go`、`apps/build/rules.go`共同内聚 `capabilities/server/studio/build/build_formulas.go`；extension `validateOf`按当前 `apps/build/objects.go`的对象责任内聚 `capabilities/server/studio/build/build_objects.go`。测试随owner迁移后删除三个旧formula路径，不重建已退出的apps/build路径；`go list -deps`与原owner测试确认纯rules无非标准库/I/O依赖，旧数值及冻结语法不变。
- [ ] 3.2 通过已有 `cmd/rule-browser` 和 `scripts/rule-browser.mjs` 构建同源浏览器模块；在原表单/页面联动入口接入获权快照与显式输入/时间，保留缺失、withheld、不完整和模块失败；扩展原一致性向量及适用表单测试，Go与浏览器运行相同向量，初始化失败不产生云端或其他隐式执行回退。
- [ ] 3.3 保留 `cmd/wasm-worker`、builder与原WASIp1 command/data ABI隔离；核对原Worker相关owner测试及部署依赖声明，浏览器预览不引用整套platformserver，也不从前端模块结果取得正式写入或授权；同步原执行说明，不另建WASM服务或求值器。

## 4. 跨层交付验证与收口

- [ ] 4.1 运行 `make check`、`make check-web`、受影响Go/Web owner检查与 `scripts/verify.sh composition`，并用一条通用kit浏览器路线完成同一草稿源码/可视化、冻结激活、原获权动作、宽窄呈现、本地公式联动及中英文切换（菜单/字段/WidgetSpec声明文字与记录业务值分离，冻结后的词典草稿不影响正式显示）；不写行业专用宿主测试，不用同源规则通过替代页面迁移证据。
- [ ] 4.2 运行适用 `make rehearse`，需要PostgreSQL的用例明确设置真实数据库；核对旧/新页面原件、候选/包摘要、检查点和原请求重试，未配置或跳过的检查如实报告，恢复不重跑规则/授权/外部请求。
- [ ] 4.3 复核附表A–F的所有删除/合并/保留目标，记录实际非测试文件数、LOC和依赖变化，更新Architecture的真实代码地图与必要操作说明；性能仅报告实际测量，不把文件减少、向量数或目标流量比例当容量/成本证明；规范、引用及差异检查通过后提交审阅。

## Workflow follow-up

- 负责人已明确授权apply；按真实实现和验证更新任务，未完成部分不能以规划或基础类型迁移替代。
- 实施及适用验证完成后使用官方 sync/archive 技能同步本change四个模块差量并归档；提交、推送和部署沿届时授权。
