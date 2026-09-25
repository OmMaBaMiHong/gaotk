# Token 银行实现与验收

日期：2026-09-25。工作目录：`/Users/wade/work-space/sub2api`，主分支：`main`。本次为代码实现、测试与推送，不部署生产。

## 需求与实现对应

| 要求 | 实现 | 验证 |
| --- | --- | --- |
| 分账独立，按策略模板走 | `service/token_bank_strategy.go` 定义接口、模板选择入口与比例策略；`repository/token_bank_settlement.go` 独立执行账本和入账 | 策略精度与边界测试；原扣费 repo 仅增加两个调用 |
| 延用原 Token 计费 | 使用原统一量化的 BalanceCost / SubscriptionCost；保留 BillingType | 原计费单测、余额及订阅数据库测试 |
| 出租者八成、Admin 两成 | Owner=round(B×8000/10000,8)，Admin=B−Owner | 消费者 100→98.75；出租者 10→11；Admin 0→0.25 |
| 请求重复不重复入账 | 复用原幂等键；独立账本 request_id+api_key_id 唯一键 | 重试只入账一次；删除幂等记录后再次请求，账本拒绝且扣费回滚 |
| 并发一致 | 先按用户 ID 排序锁定参与者，再扣款、写账本及双方入账 | 20 个并发调用重复两笔互相供给请求，双方余额均为 99.8 |
| 在途请求用原归属和比例 | 调度账号携带 RentalSnapshot，指纹纳入快照 | 暂停并把比例改成 50% 后，原在途请求仍按 80% 结算 |
| 多账号与同平台池 | accounts 归属字段；官方验证后创建；数据库平台与分组约束 | 错分组、错平台、改归属、清空分组拒绝；事务内重绑正常；影子账号继承、批量读取、暂停联动测试 |
| C 端只看自己的供给 | JWT subject 决定 owner；独立安全 DTO | HTTP 未登录 401；owner_user_id=999 不覆盖登录用户 12；跨用户账号查询/暂停拒绝 |
| 官方授权与凭据隔离 | OpenAI/Claude OAuth；三平台 API Key 官方 models 验证；Redis 会话绑定用户/state/有效期/单次使用 | 官方固定域名与重定向拒绝测试；越权、state、到期和重复会话测试；同一身份重新授权测试 |
| C 端与后台页面 | 菜单、概览、账号用量、收益流水、暂停/恢复、平台/状态筛选；后台分组/收款 Admin 配置及所属用户筛选 | Vue/TS 构建、ESLint、i18n 测试；Playwright 桌面/390px 手机及管理端检查 |
| 不暴露新账号私有信息给插件 | 插件账号快照清除出租身份、用户归属、规则和收益接收人 | 原导出字段分类测试及私有字段空值断言 |

## 执行的检查

- `go test -tags=unit ./internal/service -count=1 -json`：通过，14854 个测试/子测试通过，4 个既有跳过；没有失败。
- `go test -tags=unit ./internal/repository -count=1 -json`：通过，1024 个测试/子测试通过。
- 其余 `cmd/server`、`handler/...`、`server/...` 单测通过；新增 HTTP 所属用户隔离测试通过。
- `go test -tags=integration ./internal/repository -run 'TestTokenBankIntegration|TestUsageBillingRepositoryApply' -count=1`：通过。使用 Testcontainers 独立 PostgreSQL/Redis，并应用完整迁移。
- `go build ./cmd/server`：通过。
- `pnpm run build`：通过，包含 i18n 键完整性、Vue/TypeScript 与 Vite。
- 新增前端回调解析测试与 i18n 编译/完整性测试：8 项通过；新增页面/API ESLint 通过。
- `git diff --check`：通过。

浏览器截图位于本地 `output/playwright/token-bank-desktop.png`、`token-bank-mobile-dialog.png`、`token-bank-admin.png`。浏览器使用模拟 API 数据，不是生产数据，也不作为资金验收证据；资金证据来自上述真实数据库测试。

## 使用与边界

- 迁移不会创建已启用策略。管理员先建立对应平台出租分组，再配置 Admin 收款用户并启用；消费渠道/组合路由须接到出租分组。
- 计费、收入使用系统 USD 账本单位；今日按北京时间计算。零金额不分账，订阅是额度消耗而非该次现金实收，自用/非零赠送余额/已有透支账单沿用同一分配规则。
- API Key 指纹只能识别相同 Key，无法证明不同 Key 属于同一真实账号。OAuth 使用官方返回的账号身份。不开放变更出租所有者或更换成另一上游身份。
- 尚未执行真实外部账号 OAuth/密钥授权或生产真实请求；需要实际账号授权后才能验证供应商当时的可用性。没有生产迁移、生产余额变更或上线。
- 按仓库要求执行 `graphify update .`：AST 扫描完成，但 HTML 可视化因 64464 个节点过大而失败；不影响构建/测试。
