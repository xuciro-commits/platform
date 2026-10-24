# 楼宇供暖应用包 / Heating application bundle

一份定义，装进任何运行 `build` 应用的租户：五个对象、五个页面、一个应用。
它不依赖 Go、React 或任何宿主代码——装完即可从工作区的应用导航里打开。

安装（需要该租户里一个有 `builder` 角色的成员令牌）：

```sh
go run ./capabilities/server/cmd/install-bundle \
  -host http://127.0.0.1:8495 -tenant workspace -principal builder \
  -token-file /tmp/tok deploy/heating/application.json
```

安装器做的是人在界面里做的同一组决定：`build.object.create` → `publish`，
每个页面、最后应用。它读 `/v1/actions` 拿各 schema 的内容版本，按
`bundle-<tenant>-<n>` 做幂等键，所以重跑不会产生第二份定义。

装完后：

```sh
curl -sS -H "Authorization: Bearer $TOKEN" -H "Platform-Tenant: workspace" \
  http://127.0.0.1:8495/v1/applications        # 出现 heating，五页
curl -sS -X POST -H "Authorization: Bearer $TOKEN" -H "Platform-Tenant: workspace" \
  -H 'Content-Type: application/json' -d '{"app":"heating","page":"workorders"}' \
  http://127.0.0.1:8495/v1/page-view           # 列表/详情/六个动作
```

## 里面有什么

| 对象 | 作用 | 关系 |
| --- | --- | --- |
| `build.building` | 一栋供热建筑 | — |
| `build.zone` | 立管/环路 | → building（反向 `zones`） |
| `build.meter` | 热量表/水表 | → zone（反向 `meters`） |
| `build.reading` | 一次抄表 | → meter（反向 `readings`） |
| `build.workorder` | 工单：报修到修复 | → zone（反向 `workorders`） |

`build.workorder` 带状态机：`open → assigned → done`，`open|assigned → cancelled`，
动作 `assign`（输入 assignee）、`complete`（输入 resolution）、`cancel`。
非法转移由宿主按定义拒绝，例如 `WRK-1 is done; cancel takes it only from open, assigned`。

## 实装记录（`workspace` 租户，2026-10-24）

- 22 次决定、2.2 秒装完：5 个对象 + 5 个页面 + 1 个应用，各 create/publish。
- `GET /v1/applications` → `build heating tenant ['buildings','zones','workorders','meters','readings']`。
- `POST /v1/page-view {"app":"heating","page":"workorders"}` → `list-detail`，列表
  `summary,priority,assignee,state`，动作 `create/edit/assign/complete/cancel/archive`。
- 业务跑通：建楼 `BLD-1` → 建区 `ZON-1` → 表 `MTR-1` → 抄表 `RDG-1` → 工单 `WRK-1`
  → `assign`（A. Yilmaz）→ `complete`（换立管排气阀）；`done` 上再 `cancel` 被拒 409。
