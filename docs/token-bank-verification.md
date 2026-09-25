# Token 银行复用改造验收

日期：2026-09-25。目录 `/Users/wade/work-space/sub2api`，主分支 `main`。仅本地验收，不部署生产。

## 实现

- 删除独立 TokenBankService 账号创建/授权、独立策略表及后台银行账号页。原账号增加 `owner_user_id`，原账号创建/导入/授权/管理组件和服务复用，C 端接口增加本人权限范围。
- 原渠道 `features_config.token_savings` 保存储蓄开关、分成比例、收款管理员和接收分组。可多分组、多渠道；按实际请求分组选择已捕获渠道分账快照。接收分组仅用于新账号自动归组。
- 原计费及消费者扣费保持；独立策略分配已计费金额，同事务入账且复用原幂等键。消费 10、账号成本统计 6，80% 分成仍为用户 8、平台 2。
- 成本沿用原 `account_stats_cost` / `account_rate_multiplier` 和统计服务，保留渠道账号统计定价、默认估算及历史记录。
- 迁移 243 将旧策略配置迁入原渠道，保留历史收益流水；冲突比例明确报错。移除重复账号策略字段，保留 owner 字段。渠道变更刷新父账号/影子账号调度快照。
- 原管理端账号页增加所属用户筛选；C 端用原账号页组件查看自己的授权资料、用量、成本、使用记录、收益。

## 已执行自动检查

- `go test -tags=unit ./internal/service/... -count=1` 通过。
- `go test -tags=unit ./internal/repository/... -count=1` 通过。
- `go test -tags=unit ./internal/handler/... ./internal/server/... ./cmd/server/... -count=1` 通过。
- 真实 PostgreSQL/Redis 收益 integration 9 项通过：消费10/成本6分8/2、多渠道比例、幂等/并发/回滚/订阅/免费、归属隔离、父影子账号、渠道启停与在途快照。
- 前端共享 Create/Edit/Usage、接口 scope 和账号列表聚焦回归 172 项通过；最终 vue-tsc 与 Vite 构建通过。
- 真实 PostgreSQL 列表范围测试通过：本人隔离、分页总数、管理员 owner 筛选、管理员原列表保持。

## 验收边界

自动测试使用独立容器，不作为供应商授权可用性证明。未使用真实供应商凭据调用外部模型。渠道接收配置由管理员决定；用户上传的成本不改变平台分成基数。删除/停用账号沿用原生命周期，已发生收益流水保留。
