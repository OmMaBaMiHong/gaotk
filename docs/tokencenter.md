# TokenCenter 目录模块

目录属于 TokenCenter 自己，使用 PostgreSQL `tokencenter_catalog` 单行 JSONB 快照保存，不依赖网关计费数据或 Redis。主服务和独立启动器都通过 `TOKENCENTER_DATABASE_URL` 连接专用 TokenCenter 数据库，模块自行迁移 `internal/tokencenter/catalog_schema.sql`；不使用账号服务的 SQL 连接和迁移。主服务未配置该 URL 时不注册目录路由。独立启动器不初始化账号业务表，不创建数据库或基础设施。

## 路由

- `GET /api/v1/tokencenter/catalog`：公开完整目录，直接返回 Catalog，无 `data` 包裹。
- `GET /api/v1/admin/tokencenter/catalog`：鉴权后读取完整目录。
- `POST /api/v1/admin/tokencenter/import`：请求体为完整 Catalog，校验成功后原子覆盖。
- `POST /api/v1/admin/tokencenter/sync`：从明确配置的源拉取完整 Catalog。没有配置源返回 503，失败返回 502 且保留最后成功快照。

主服务管理接口沿用管理员 JWT（`Authorization: Bearer ...`）或 Admin API Key（`x-api-key`），并经过限流、审计和现有合规中间件。独立启动器仅接受环境配置的开发 Bearer token。模块错误形如 `{"error":"..."}`；主服务鉴权中间件维持原有错误格式。

Catalog 字段为 `version, updatedAt, source, demo, models[], sites[], offers[]`。Model 的 `context` 是 Token 数字，`input/output` 是每百万 Token 美元报价；Site 的 `uptime/latency` 可为 null；Offer 包含 `id, modelId, siteId, input, output, currency:"USD", unit:"1M tokens", updatedAt, source`。来源发布日期保留原精度，`released` 可为 `YYYY-MM-DD` 或中央来源明确给出的 `YYYY-MM`，更新时间采用 RFC3339；未知为 null。

当 `rankingAuthority:"chaos"` 时，`rankings` 保留中台的榜单版本、顺序、名次、结果与单位，本站不计算能力排名。价格、上下文、发布日期、观测时间可以为 null，明确的 0 保留为 0。`sourceSnapshot` 保留产品、配置与发布版本；`evaluations` 保留模型固定版本和每个供应商的真实测评；`officialSubscriptions` 保留官方订阅条件和原文证据；`subscriptionMerchants` 保留商家原始SKU、报价、币种/库存未知值与公开证据，不能作为官方套餐或本站交易担保。厂商、聚合渠道、二手目录由 `providerKind/sourceAuthority` 区分。遗留 Catalog 格式仍按原 score 排序，固定样例只用于显式 demo。报价不通过站点倍率推演。

管理导入保留原有 8 MiB 请求上限。内部完整 Chaos 同步及数据库重载不按该请求上限截断；分组/时段/缓存阶梯可有多条独立 Offer ID。校验拒绝未知字段、非法数值、重复 ID、无效引用和无效货币/单位；未知来源值保留 null。无效导入不改变数据库。同步的 ETag / Last-Modified 与成功快照一起原子保存；失败不推进条件请求状态。重启后从数据库读取目录和条件请求元数据。

## 独立启动器环境

| 变量 | 默认值 / 用途 |
| --- | --- |
| `TOKENCENTER_DATABASE_URL` | 必填，已经创建的专用 TokenCenter PostgreSQL 库 URL |
| `TOKENCENTER_ADMIN_TOKEN` | 必填，开发管理接口的 Bearer token |
| `TOKENCENTER_ADDR` | `127.0.0.1:8086`；启动器强制使用端口 8086 |
| `TOKENCENTER_SEED_DEMO` | `false`；true 时仅空库写入嵌入示例，不覆盖已有快照 |
| `TOKENCENTER_SOURCE_URL` | 空；完整 Catalog JSON 源，仅允许明确配置的 http/https URL，无 URL 内凭据/fragment |
| `TOKENCENTER_SOURCE_FORMAT` | `catalog`；`chaos` 使用通用产品快照接口并读取全部分页 |
| `TOKENCENTER_SOURCE_TOKEN` | 空；可选源 Bearer token，要求 HTTPS；不发送给重定向目标 |
| `TOKENCENTER_SYNC_INTERVAL` | `15m`，正 duration |
| `TOKENCENTER_SYNC_TIMEOUT` | `20s`，正 duration |
| `TOKENCENTER_TEST_DATABASE_URL` | 可选真实 PG 测试连接；数据库名称必须为 `tokencenter_test` |

主服务也读取 `TOKENCENTER_DATABASE_URL` 和 seed/source/format/interval/timeout；目录连接池单独创建、迁移和关闭，管理员身份仍使用主服务的既有鉴权。独立启动器使用开发 token。

Chaos 同步先读取产品配置，再读取所有材料与榜单分页；固定首个 publicationRevision/version/configRevision，各页一致才原子保存，任何失败或版本漂移保留旧快照。配置源后会启动即刻同步和定时同步。Chaos成功发布的 `delivery.pullIntervalSeconds` 动态替换启动配置的fallback；相同周期不重复重建timer，失败保留上次成功周期，Stop/取消立即终止等待。未配置源时没有同步 worker，也不声称接入 Chaos 等外部数据。同步不跟随重定向，管理请求体无法指定任意抓取地址。更新失败继续服务数据库内旧快照。worker 在主服务初始化失败或退出时停止。

先由操作者在共享 PostgreSQL 登记专用库及账号，再在已安全设置环境变量的 shell 中运行：

```sh
cd /Users/wade/work-space/sub2api/backend
TOKENCENTER_SEED_DEMO=true go run ./cmd/tokencenter
```

这条命令要求事先设置 `TOKENCENTER_DATABASE_URL`、`TOKENCENTER_ADMIN_TOKEN`，缺少配置或数据库连接失败立即退出。不要修改或重新绑定承载 Skoob 的现有 8080 账号服务，不新建 PG/Redis 容器。

示例文件：`backend/internal/tokencenter/demo.json`。包含 3 个虚构模型、2 个虚构站点和 5 条固定示例报价，`demo:true` 且 source 明确声明 demo，无真实监测 uptime/latency。前端必须显示 source/demo。

## 验证

```sh
cd /Users/wade/work-space/sub2api/backend
go test ./internal/tokencenter ./cmd/tokencenter ./cmd/server -count=1
go test ./internal/server/... ./migrations -tags unit -count=1
go test -race ./internal/tokencenter ./cmd/tokencenter -count=1
go build ./cmd/tokencenter ./cmd/server
```

默认测试使用 memory store、SQL mock 和 httptest，覆盖严格校验、排序/覆盖数、HTTP 鉴权与响应、失败保留、成功条件元数据、超时、定时同步、禁止重定向及 seed 不覆盖。SQL mock 验证单语句原子写入和新建 service/store 后读取。

真实 PG 重连持久化测试只有设置 `TOKENCENTER_TEST_DATABASE_URL` 时执行；未配置明确 skip。测试使用专用 `tokencenter_test` 库内临时 schema，关闭并重开连接池后读取，随后删除该测试自身 schema；不会建立数据库或启动容器。本次在共享 PG 内的专用 `tokencenter_test` 数据库执行过该测试；mock 测试本身不代表真实 PG 联调。

## 2026-11-03 本地真实接线

共享PG的专属 `tokencenter` 库与账号已启用，模块未使用 `skoob_accounts` 账号连接。原8080服务保持不动，开发启动器监听8086，Next网页3020，Chaos统一Go应用4000。真实来源、数量、中央榜单策略及网页证据统一记录在TokenCenter仓库的 `docs/真实目录MVP交付验收-2026-11-03.md`。这次只完成本地链路，不代表生产账号服务已部署新版本。
