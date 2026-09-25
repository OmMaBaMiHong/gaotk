# Token 银行：用户出租账号与按调用分账

日期：2026-09-25。状态：源码核查与待确认方案，尚未实现或部署 Token 银行。

## 1. 需求与边界

- C 端新增「Token 银行」菜单，用户可以授权或导入多个自己的上游账号。
- 账号进入本站出租池，实际有请求命中该账号时，按平台规则计算出租者收益。
- 现有调用者 Token 计费继续复用；用户提出出租者 80%、平台 20%。
- 出租者查看自己的账号状态、调用用量、收益流水与累计收益；收益进入其余额。
- 管理端账号列表增加所属用户、来源、出租状态筛选，配置出租渠道和各平台规则。
- DeepSeek 账号只能进入 DeepSeek 出租分组；平台、分组、价格、分成不能由客户端任意指定。
- 本轮只完成上游更新整合与设计核查，不创建生产账号、分组、价格或余额流水。

待用户明确的两项：分账基数是调用者现有实际计费金额，还是按平台另设 Token 结算价；收益是否仅用于站内余额，还是还需提现。未回复前，本文以「现有实际计费金额分 80%，进入站内余额」作为讨论方案，不能视为最终规则。

## 2. 已核实的基础能力

| 模块 | 源码现状 | 可复用与缺口 |
| --- | --- | --- |
| 账号 | `backend/ent/schema/account.go` 有 platform、type、credentials、调度状态、rate_multiplier、parent_account_id，无所属用户 | 复用 accounts；需要新增 owner_user_id 等归属信息 |
| 账号授权 | `backend/internal/server/routes/admin.go` 提供 Claude/OpenAI 等授权和账号创建接口；DeepSeek 有独立平台与 API Key 能力 | 复用服务层；新增 C 端授权会话和受限接口，不能把 admin 接口直接暴露 |
| 调度与分组 | accounts 通过 account_groups 进入 groups；渠道通过分组配置定价 | 复用现有调度器，建立各平台专用出租分组 |
| 计费明细 | `backend/ent/schema/usage_log.go` 记录 user_id、account_id、request_id、Token 分类、total_cost、actual_cost 等 | 能追溯消费账号，但 user_id 指消费者，不是出租者 |
| 原子扣费 | `backend/internal/repository/usage_billing_repo.go:Apply` 在一个 SQL 事务内做幂等占位、扣余额/订阅额度、更新限额 | 最适合接入出租分账；当前没有出租者入账 |
| 金额精度 | `backend/internal/service/usage_billing.go` 对齐 NUMERIC(20,8)，已有 decimal 量化工具 | 复用精度规则，避免各处独立浮点舍入 |
| 账号统计 | `backend/internal/service/account_stats_pricing.go` 有账号独立定价优先级 | 可复用用量聚合，但统计成本不自动等于结算基数 |
| 用户余额/返利 | `affiliate_repo.go` 有返利额度转余额的事务和账本；`admin_user.go` 有管理员加余额 | 参考实现方式；不把出租收益伪装为充值或邀请返利 |
| C 端菜单 | `frontend/src/components/layout/AppSidebar.vue` 和 `frontend/src/router/index.ts` 区分用户/管理员菜单 | 增加 Token 银行页面和用户 API |

当前 accounts 由管理员统一管理，不是数据库里每个账号都已经绑定超级管理员。旧账号 owner_user_id 为空即可保留自营语义，无需批量绑定某位管理员。

## 3. 渠道和真实性

建议复用一个「Token 银行」渠道，关联 OpenAI、Anthropic、DeepSeek 等各自独立的出租分组。渠道定义价格与展示，实际选账号仍由现有 group/account 调度完成。已有普通消费者必须通过相应分组或已有的组合路由进入出租池；单独建渠道不会自动获得全站流量。

用户选择平台后，由服务端选择该平台已启用的出租规则和分组。创建、重新授权、改绑、管理员批量操作和影子账号派生都要维持同一约束，调度时也不能绕过平台限制。现有 `checkMixedChannelRisk` 主要检查 Antigravity/Anthropic 混合风险，且可确认跳过，并不满足此次全部平台的硬约束。

只检查用户填的 platform 不足以保证真实性。出租入口应复用平台官方 OAuth 或官方 API Key 验证，限制官方端点及平台允许模型，不开放自定义 base_url、任意模型映射、代理或请求头给出租者；不提供 OAuth 的平台使用已有密钥接入方式。记录验证后的平台/上游身份并防重复挂载。同一个真实账号的多个 Key 或影子账号不能重复归属不同出租者。

## 4. 最小数据结构

### accounts 的扩展

- `owner_user_id`：可空，引用本站 users；由服务端当前登录用户写入。
- `rental_status`：未出租、待验证、启用、暂停；与现有账号故障/限流状态分开。
- `rental_policy_id`：关联该平台的出租规则。旧账号均为空，保持自营行为。
- 已验证的上游账号身份/密钥指纹：复用能验证的现有身份数据；只有平台接口可证明时才宣称识别真实账号。不能用用户输入昵称判断重复。

### account_rental_policies（新表）

管理员配置平台、目标分组、出租者比例（初始 8000 基点）、平台收款用户 ID（若要求 Admin 余额到账）、启用状态、规则版本。客户端不能提交或修改这些值。

若确定「另设结算价」，优先评估现有渠道账号统计定价规则的复用，明确输入/输出/缓存单价及生效时间；不能把一个平台所有 Token 的成本假定相同。结算价格必须持久化快照，不因后台改价重算历史收益。

### account_revenue_ledger（新表，逐笔结算账本）

记录 request_id、api_key_id、account_id、实际凭据所属账号 ID（影子账号需归回所有者）、owner_user_id、platform、group_id、policy/version、消费者计费金额与计费类型、结算基数、比例快照、出租者金额、平台金额、收款 Admin ID、入账/冲正类型、原流水关联和时间。金额使用 NUMERIC(20,8)。

沿用现有计费事件粒度，唯一键采用 request_id + api_key_id + 事件类型；不要自行按每次网络重试生成新 ID。消费与独立工具事件分别遵循原计费事件 ID。账本不跟随 usage_logs 的日志清理删除，也不通过重新求和可变价格来决定已入账收益。

一期不需要通用银行、借贷、利息或理财产品引擎。名称使用「Token 银行」，收益来源是账号实际调用收入。

## 5. 分账口径

推荐讨论方案：继续用现有渠道/模型计价及倍率计算调用者实际账单。余额计费时，以统一量化后的 BalanceCost 为基数 B：

- 出租者收益 U = round(B × 0.8, 8)。
- 平台分成 A = B − U，保证精确相加等于 B。
- 例如 B=1.00000000，则 U=0.80000000、A=0.20000000（单位与系统原账本一致）。
- 管理端看到平台分成流水；如果用户要求平台 20% 也进入 Admin 余额，则将 A 原子加到明确指定的 Admin 收款用户，记录该 ID。

不能使用账号 rate_multiplier 直接代替 80% 分成：它影响账号配额/成本统计，不会给出租者入账，也不改变消费者分组倍率。

若另设出租结算价 C，则 U=C×80%；消费者账单仍为 B，平台实际留存是 B−U，不一定是 B 的 20%。要维持严格二八分，结算基数必须与消费者计费一致，或明确补贴/差额由平台承担。

订阅额度消耗不是本次现金收款；免费/赠送额度、透支请求及自用命中也要在实现前明确结算规则。不可默默把 SubscriptionCost、AccountStatsCost 都当实收分账。一期建议优先完成余额计费闭环，其它计费类型是否准入由用户确认，不改变现有消费者计费规则。

## 6. 事务接入与可追溯性

请求命中账号 → 原计费器产出金额 → `UsageBillingRepository.Apply` 事务内：

1. 按现有请求 ID/指纹防重，已执行的事件不再扣款或分账。
2. 完成原有消费者余额扣款、Key/账号额度更新。
3. 对具有有效出租归属且符合结算规则的账号，写分账账本并增加出租者余额；如配置了 Admin 收款用户，同时增加平台余额。
4. 一次提交；失败则全部回滚。统一用户行锁顺序，合并同一用户的扣款/收入，覆盖消费者同时是出租者或 Admin 的情况。
5. 提交后失效/刷新所有受影响用户的余额及认证缓存；不能只更新消费者缓存。

归属和规则应在请求计费上下文中保存快照，禁止通过结算时重新读取已修改的 owner 把历史流量收益转给新用户。暂停只阻止新调度，已完成请求按原快照结算；换绑所有者一期不向用户开放。

现有 usage_logs 在扣费事务外写入，虽有同步兜底，也不是资金账本。不能在写日志成功回调或定时扫描日志时简单 AddBalance，否则重复、漏记或日志清理会改变资金结果。用量图可查 usage_logs，收益和对账必须查独立结算账本。

出租收益使用独立来源类型，不能调用「管理员充值」路径触发邀请返利、累计充值或会员资格副作用。若之后需要退款/纠错，用关联原流水的冲正，保留原始记录。

## 7. 页面与权限

C 端一个 Token 银行入口即可：顶部展示今日/累计收益与出租账号数量，列表展示平台、账号名、授权/调度状态、调用量和收益；支持新增授权、重新授权、暂停/恢复、查看单账号明细。支持一个用户多个账号，收益自动进入现有余额。

后台沿用账号管理，增加所属用户、出租来源、出租状态、平台筛选和单账号分账明细；渠道配置维护各平台价格/分组和统一分成，汇总展示出租者收益与平台分成。

新增受 JWT 认证的 `/api/v1/user/token-bank/*` 用户接口。每一次详情、统计、暂停、重新授权都在服务端按当前用户检查 owner_user_id。前端不传任意 owner_id，不使用管理员 DTO 直接回传 credentials。出租者只看到其供给的模型、Token、时间与收益，不看到调用者 API Key、IP、提示词或调用者私人资料。原 `/usage` 页面仍表示「我消费了什么」，Token 银行表示「我的账号被使用了多少」。

授权会话必须绑定当前用户和平台，校验 state/PKCE/过期与单次使用；凭据保存在后端并复用刷新机制，列表不回传原始 token。后端决定允许的类型、分组和费率。

## 8. 实施步骤与验收

1. 上游整合：独立 worktree 合并 origin/main，保留定制；验证冲突处理、服务端测试、前端构建。
2. 归属与路由：migration/schema/repository/缓存投影/DTO 一起扩展；验收旧账号正常、多账号归属、跨用户拒绝、DeepSeek 错组拒绝、影子归属一致。
3. 分账：在既有扣费事务增加账本与双方余额；验收二八精确相加、重复请求只结算一次、并发一致、任一写入失败整体回滚、改价不改旧收益、暂停时在途请求正常结算、缓存正确更新。
4. C 端与后台：接入现有授权服务及统计；验收登录用户只能见自己的账号/收入、管理员可筛选，实际调用命中某出租账号后消费者扣款、出租者入账、平台分成和页面展示一致。
5. 发布：测试环境完成真实平台授权与实际调用验证，核对 PostgreSQL 账本和余额后再安排生产迁移与部署。

## 9. 本轮上游整合记录

- 源仓库：origin = https://github.com/Wei-Shaw/sub2api.git。
- 拉取到 origin/main：a3eb7ef30，VERSION=0.2.8（远端提交时间 2026-09-23）。
- 原工作区分支：codex/skoob-membership-key-filter，HEAD=ed1b287db，有未提交会员 Key 改动，保持原样。
- 独立分支：codex/token-bank-upstream。
- worktree：/Users/wade/.config/superpowers/worktrees/sub2api/token-bank-upstream。
- 上游与本地已提交代码分叉：本地 22 / 上游 235 个提交。
- 合并冲突：wire_gen.go 保留 Kimi 注入并接入上游 OpenCode Go 用量服务；docker-compose.yml 同时保留本地 OAuth 配置与上游 simple-mode 配置。
- 新旧测试签名适配：UpdateUserBalance 补既有 adjustmentType 空值；TokenRefreshService 测试补既有 KimiOAuthService 参数。
- graphify update . 已尝试，工具报 No code files found，未刷新知识图；源码结论来自实际文件核对。
- 合并提交：0a9fc81ef（本地）。本分支未推送，未部署生产。
- `go test -tags=unit ./cmd/server ./internal/server/... ./internal/service/... ./internal/repository/...` 全部通过；service 包用时 184.419 秒。
- `go build ./cmd/server` 通过。
- `pnpm run build` 通过，含 3 项 i18n 测试、Vue/TypeScript 编译和 Vite 构建；仅有现有构建警告。
- `docker compose -f deploy/docker-compose.yml config --quiet` 通过（使用临时占位数据库密码，仅解析配置，不启动服务）。
- `git diff --cached --check` 通过。未运行生产迁移或真实出租调用验收，因为 Token 银行尚未实现。
