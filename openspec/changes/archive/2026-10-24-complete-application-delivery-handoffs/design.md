# Design

## Context

证据见审计 F03/F10–F12。三个交接对象分别是完整选择、封存候选、确切批量意图；当前界面却依赖路由小上限、Builder 草稿和瞬时 selection。模板任务同样把完整应用资产替换为页面布局。宿主既有 128 项联合草稿、64 项 Composite、候选及包预检，不需要新增执行体系。

## Goals / Non-Goals

**Goals:** 从应用起步到发布/退役，跨入口与角色交出的仍是用户审阅的原对象。

**Non-Goals:** 不扩大 Publisher 草稿权、不增加 Composite 上限、不增加离线批量执行、不改变内核或安装语义。

## Decisions

1. **同壳交付用原 ViewTransfers。** Project/Changes 将完整类型化选择交给既有视图调用；URL 只承载已有 ticket。ReleaseReview 在原任务接收并固定选择。保留旧合法 JSON/旧 ID 链接解码，删除过时 32 限制。`release.tsx:188/220` 的 4096B 检查也须校准至原宿主 `releaseCommandBytes=128×512=65536B`，统一检查实际请求元数据字节，不只改 URL。直接交接允许原 128 项及合法长 UUID；超限明确拒绝，不截断。刷新后过期 ticket 明确提示回原任务，不猜一个应用根。可分享的正式交接使用候选身份。
2. **Publisher 只消费已封存内容。** Build 的草稿 Changes 菜单只对原 Builder 开放；Publisher 首页/菜单进入候选与历史。显式增加候选 ID 路由参数，并在首次挂载/刷新时调用原 readSaved；现有 `kind/id/drafts` 中的 id 继续表示草稿，不挪用。候选过滤使用独立正式 `applicationName`，不把 `application=<build.app草稿ID>` 当它必须读取的上下文。旧带草稿 scope 的链接若没有可验证候选，提示重新选择获权候选，不因 scope 等不到草稿而永远 Loading。应用草稿 ID 与正式应用名继续区分，不额外造映射数据库。
3. **冻结批次再调用原 Composite。** 确认前固定完整 items；65 项直接阻止，用户重新选定不超过 64 项的明确批次。一次原任务保留 `{key,edits}`，提交/未知重试复用；确定拒绝后显式修正才建新意图。复用 ReleaseReview 保存 pending 请求和原项目 busy/unsaved 模式，未知不当成功。当前 CompositeEdit 没有 expectedRevision 字段，本 change 不宣称增加修订条件或发明新请求队列；正式业务仍由原 owner 检查。
4. **应用模板复用包流程。** New from template 导向原包选择/预检与应用草稿生成，保留所选包确切版本和回到新应用的定位。无安装资格只提供原申请/审阅入口；不扩大权限。页面模板仍保留在应用 Interface 的页面作者区，移除新项目入口对 pageTemplates 的误用，不删除页面模板能力。

## Removal and Preservation

删除 release decoder 的 32 项门槛、把长完整选择放 URL 的新生成路径、Publisher 草稿菜单/等待、退役 `slice(0,64)` 和每次重试换键。合并模板起步到已存在的包→草稿链。保留旧可解析链接、确切候选、评估/升级门禁、原资源加入失败重试、包三种来源、审批、历史和 K5。

## Dependencies and Ownership

本 change 独占 Build 的 Publisher、交付和批量退役接线；`align-workspace-discovery-and-outcomes` 不重复修改这些菜单。只消费 `retain-workspace-task-context` 的通用离开保护，不替它重建会话。原 `unify-directory-pages` 只做目录归因，本 change 不重做分类体系；`consolidate-platform-boundaries` 的包/宿主组织不变。

## Risks / Trade-offs

- [ViewTransfers 在刷新后不持久] → 明确过期并保留原草稿；保存候选后通过封存 ID 恢复，不新增全局选择存储。
- [批量未知被错误当明确拒绝] → 对网络/未知保持原 pending；检查同键原答复，再允许编辑。
- [模板包不含所有可能场景] → 如实展示包边界，不自动补业务定义，不用页面壳声称可运行。

## Migration Plan

先修发布选择与 Publisher 交接，再修批量意图和模板入口；每个模块带对应回归。旧历史字节、发布候选、已建空应用及页面模板保留，不自动迁移或播种。需要生成物时用原生成器，禁止手改。
