# DeepSeek 统一模型配置（2026-09-20）

## 对外与对内

DeepSeek 分组 15 的公开模型列表仅包含 `deepseek-flash` 与 `deepseek-v4-pro`。
渠道 3 只负责旧公开名称归一化；具体供应商型号由账号的 `credentials.model_mapping` 决定。

| 账号 | 对外 Flash | 对外 Pro |
| --- | --- | --- |
| 107 火山 | deepseek-v4-1-flash-260910 | deepseek-v4-pro-ga-260813 |
| 113、115 官方 | deepseek-flash | deepseek-v4-pro |

旧调用 `deepseek-v4-1-flash`、`deepseek-v4-flash` 经渠道映射到 `deepseek-flash`，不出现在公开列表。旧 V4 Flash 名称现由 V4.1 Flash 提供服务，不再指向火山旧 260731 型号。这与当前官方旧 Flash 名称的迁移方向一致：<https://api-docs.deepseek.com/zh-cn/quick_start/pricing/>。

不改 Key、账号优先级、分组权限、价格配置、OAuth 或其他模型分组。新增上游账号时，映射左侧仍使用这两个统一名称，右侧填写供应商实际型号，不能把供应商别名再放到左侧。

## 实施与回退

- `deepseek-canonical-routing.sql` 是本次线上一次性配置变更，包含旧值和分组绑定校验；重复执行会拒绝，避免覆盖后续修改。
- 修改前备份：服务器 `/opt/sub2api/ops-backups/20260920-141553-deepseek-canonical/mappings.json`，仅包含两个模型映射字段，没有密钥。
- 数据库事务修改账号 107、渠道 3，并写入 `account_changed` 调度事件。
- 提交后向 Redis `channel_cache_updated` 发布 `refresh`，无需重启；调度事件已消费。
- 回退使用同目录 `rollback.sql`，再发布同一缓存失效通知。回退前必须确认这两个字段没有后续变更。

## 线上验收

- 公网 `https://gaotk.com/v1/models` 返回 HTTP 200，严格只有两个统一 ID。
- 四个真实请求全部 HTTP 200：两个统一 ID 和两个旧 Flash 别名。
- 使用日志确认：Flash → 火山 V4.1、Pro → 火山 Pro；旧别名 → 统一 Flash → 官方账号 115 / 火山账号 107。不是仅修改展示文字。
- 本轮未单独强制测试账号 113。测试不代表所有上游未来持续可用。
- Skoob 自身有模型目录缓存；设置页“可用模型 → 刷新”会重新获取目录。

本次使用现有配置能力完成，没有修改或重发应用二进制。上游响应中的 `model` 字段仍可能包含实际供应商型号；本次统一的是公开选择列表与请求入口，调用日志保留真实路由信息。
