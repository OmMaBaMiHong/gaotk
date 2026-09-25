# Token 银行复用改造验收

日期：2026-09-25。目录 `/Users/wade/work-space/sub2api`，主分支 `main`。本文按阶段记录；最新生产发布为文末 df812db 验收。

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

## 本地 Docker 与页面验收

- 本地 ARM64 镜像内嵌前端，`127.0.0.1:18443` / `18441` 共用本地服务；PostgreSQL、Redis 数据卷保留，健康检查通过。
- 本地真实 API 验证原账号创建/列表/统计、自动绑定当前用户、按平台归组、过滤越权管理配置、跨用户 404、普通用户后台 403、未登录 401、管理员归属筛选。
- 浏览器确认旧后台银行地址跳到原账号页；原渠道表单显示用户 80% 及接收分组；C 端展示原多平台添加、原成本统计和使用记录。
- 页面放入两个明确标记“未授权，已暂停”的本地模拟账号，未伪造使用记录/收益。不接真实外部供应商，收益为空是预期。


## 2026-09-25：收益广播与排行榜本地验收

- 功能代码：`3a1f56ad4`，原目录 main 已推送 gaotk/main。
- 本地镜像：`sub2api:token-savings-local-3a1f56ad4`，容器版本确认 commit 一致，健康检查通过；原 PostgreSQL/Redis 卷和账号保留。
- 顶部一行广播、收益榜弹窗、系统设置 → 功能开关中的全局开关均已实现。默认关闭，本地临时开启供查看；线上尚未发布本次改动。
- 前端 9 项组件测试、3 项语言完整性测试、TypeScript、目标 ESLint、完整生产构建通过。
- 后端 service/handler/routes 的 TokenBank 定向测试通过；实际 PostgreSQL18/Redis 集成覆盖原有 8 个结算场景及新增榜单场景：消费 1.25 对应展示到账 1.00、跨账号累计、正数记录、前10、已删除用户排除、未提交记录不可见。
- 真实本地 API 验收：未登录 401、普通用户操作管理接口 403、非法开关输入 400、关闭/开启/关闭后立即不返回明细，切换前后本人账号与收益概览一致。
- 浏览器验证顶部广播、收益榜打开/关闭与窄屏布局；本地无真实收益显示空态，没有向运行中的账本插入演示收益。
- GPT 套餐核验及只选最高优先级分组本次仅完成审计和规则梳理，没有修改当前接收绑定逻辑；GPT 储蓄线上仍未开启。Pro 20x / Pro 5x 准入范围待确认。

## 2026-09-25：严格套餐准入与单组优先级

- 用户确认 GPT Pro ×20 仅接收 Pro20x；C 端不提供分组编辑。原渠道增加接收优先级和套餐规则，多个合格组只选最高优先级一个，同优先级按分组 ID 升序。
- 原创建、导入、重授权服务复用；服务端固定上游核验取代用户 plan_type / JWT 自声明，API Key 不冒充订阅。重授权重新归组；正常刷新保留轮换凭据并在核验失败时清除旧套餐标记。
- 调度、粘性、影子账号及 WebSocket 每轮最终准入检查复用原链路，套餐限制不改变原计价和分账。
- 前端 10 项规则测试、3 项语言测试、TypeScript、目标 ESLint、1082 模块完整生产构建通过。
- 后端 service / admin handler / gateway handler / routes 的套餐、归属、调度、刷新、利润门与 TokenBank 聚焦测试 386 项通过；新增末尾竞态检查后再跑 90 项定向回归通过。
- 真实 PostgreSQL18.1 / Redis 集成覆盖主账号与影子账号套餐快照、降级后拒绝，原 8 项结算场景保持通过。
- 未使用真实 Pro20x 账号完成供应商授权；不将模拟上游测试描述为真实 Pro 可用性证明。无新定时套餐查询，静默套餐变化需等到上游刷新或响应被观测后阻断。

## 2026-09-25：df812db 发布验收

- 代码提交 `df812db9f` 已推送 `gaotk/main`。本地镜像 `sub2api:token-savings-local-df812db` 和线上镜像 `sub2api:20260925-df812db` 均核对运行 commit 为 df812db。
- 本地真实 API 复测原 DeepSeek 创建、导入、归属隔离、统计、非法 JWT/套餐标记和 API Key 冒充 Pro 拒绝，拒绝后未落库；收益开关及权限验证通过。浏览器在原渠道表单确认优先级 100 和 Pro20x 正确回填。
- 全库与配置备份 `/root/backup-20260925-token-savings-pro20`，pg_dump 120194055 字节、pg_restore 目录 1298 行；回滚镜像 `sub2api:rollback-20260925-pro20`。镜像在本地构建为 linux/amd64，上传 SHA256 校验通过，仅替换 sub2api。
- 原 OpenAI 渠道 1 接入原 GPT Pro ×20 分组 19：严格 allowed_plans=[pro]、priority=100、用户 8000bps、Admin 1。移除该渠道对已删除分组 4 的关联；渠道模型定价保持，对外倍率 0.8 保持。DeepSeek 渠道 3 / 分组 15 保持。
- 线上健康 200、容器 healthy/0 restarts，迁移数量仍为 292；真实 DeepSeek 小请求返回 200 和有效 choices。TokenBankView、ChannelsView、SettingsView 公网产物哈希与本地构建一致。
- Skoob OAuth 客户端启用、原密钥与两个回调白名单保持；公网授权 GET 302、允许回调的匿名 POST 401、非法回调 POST 400、匿名私有接口 401。收益广播/排行榜已部署，生产总开关默认关闭，待有真实数据后开启。
- 当前尚未用真实 Pro20x 账号做成功授权验收。30% 毛利口径仍未确认，本次没有擅自更改成本倍率、利润门或 80/20 分账。

## 2026-09-25：按用户要求临时停用线上 C 端

- 产品范围调整：只考虑剩余订阅额度的 GPT、Claude 授权账号；普通 API Key 和其他平台不在下一版开放范围。此次只关闭线上，不提前重新开放新版本。
- 原渠道 1、3 的 token_savings.enabled 均设为 false；现有 1 个 owner_user_id 账号 schedulable=false。账号、余额、历史账本保留；原售价和分账比例不变。收益广播开关保持 false。
- 当前代码没有整个 Token 银行的总开关，故使用原渠道配置加 Nginx 临时封禁。线上 `/etc/nginx/conf.d/lai.gaotk.com.conf` 引入 `/etc/nginx/snippets/token-bank-disabled.conf`，内容存档在 `deploy/token-bank-disabled.nginx.conf`。配置测试通过后 reload，无应用重启。
- `/token-bank` 直接请求跳转 dashboard；C 端账号创建/导入/管理、各供应商授权及银行 API 返回 503 / TOKEN_BANK_DISABLED。旧 SPA 会话可能仍显示菜单，但接口已不可操作。后台管理、普通用户资料、API Key 管理、模型调用和 Skoob OAuth 路径未封禁。
- 公网验证：页面 302，账号与 OpenAI/Claude 授权及广播接口 503，health 200，普通 profile/admin accounts 匿名仍 401。
- 关闭后真实 DeepSeek 小请求 200 且返回 choices；Skoob OAuth 客户端、原密钥及回调保持，容器 healthy / 0 restarts。
- 关闭前配置保存在 `/root/backup-20260925-token-bank-disabled/`：channels-before.json、accounts-before.json、nginx-before.conf；含配置文件均为 root 私有。恢复必须同时明确处理 Nginx include、渠道接收开关和原暂停状态，不能单独开广播开关视为恢复完成。


## 2026-09-25：官方订阅与可切换接收政策（本地验收，未上线）

- 新增真正的 `token_bank_enabled` 总开关，默认关闭。系统设置 → 功能开关分别显示银行总开关、收益展示开关。公开设置与 HTML 首屏注入同步，C 端全体本人账号/授权/银行接口逐请求检查总开关；原管理端不受影响。
- 复用原渠道接收规则增加 `account_types`，显式空数组可关闭规则，API Key 能按未来政策显式重开，未删除原账号管理能力。当前本地配置只有 GPT OAuth（Pro）和 Claude OAuth（Pro/Max），其他类型关闭。
- 前端依照原渠道能力列表收起其他平台、API Key、setup-token 和通用导入入口；超级管理员访问 C 端也遵循相同范围。原后台管理页面保留原能力。
- Claude 复用原 OAuth 客户端，增加官方 profile 的组织身份和 Pro/Max 核验。具体证据与限制见 `token-bank-claude-verification.md`。GPT Free 和跨平台套餐值拒绝，客户端身份覆盖字段不会代替已核验令牌。
- 创建/重授权/刷新继续复用原服务；刷新套餐失效保留轮换凭据但阻止请求。修复 owner 重授权时原敏感字段合并把旧 refresh_token 补回的问题；原后台普通编辑的敏感凭据保留语义不变。
- 原计费、成本与 80/20 分账金额算法未修改。渠道关闭类型、平台不匹配、套餐不匹配均阻止调度；Claude 最终检查使用实际解析后的 fallback 分组。

验证证据：

- 后端最后一轮 6 个包共 246 项聚焦测试通过，覆盖总开关权限/即时生效、公开设置/首屏同步、原管理端凭据合并回归、owner 全流程重授权、官方核验、刷新缓存、接收与调度；另 Claude provider/refresh 独立回归 102 项通过。
- 真实 PostgreSQL/Redis `CI=true` 的 TokenBank 原结算场景和 owner/type/actual-group 快照集成通过。测试容器不替代供应商真实授权。
- 前端 13 个文件 155 项聚焦回归通过；Free/显式关闭规则修正后相关 16 项通过；TypeScript、修改文件 ESLint、生产构建通过。
- 本地 Docker 只重建 app，保留原 PostgreSQL/Redis 卷；健康检查通过。真实 HTTP 验证关闭/开启/再关闭即时拦截、普通用户不能改配置、旧 API 不能绕过、公开开关同步、接收类型空数组/API Key 的配置往返、伪造凭据拒绝且账号总数不变。
- 浏览器本地页面已确认新的闲置官方订阅文案，添加窗口只显示 Anthropic / OpenAI OAuth；系统设置存在两个独立开关。本地预览已开启供验收，原模拟账号/收益账本保留，没有注入收益。
- 线上 `/api/v1/user/accounts` 复查 503，继续维持前一节的 Nginx 临时关闭、渠道关闭及账号暂停。本次没有生产发布。

后续真实验收与发布：使用真实 GPT/Claude 官方订阅账号完成成功授权与小请求核验后再决定开放。正式发布需确认新 scheduler 快照刷新（旧缓存缺少 owner/type 字段会拒绝调度），并处理临时 Nginx include；不能只开广播开关或只移除 Nginx 封禁。总开关管用户工作区，停用供给仍使用渠道和账号调度开关。


## 2026-09-25：线上侧栏关闭同步（f121865 发布）

- 用户反馈线上关闭后侧栏仍显示。核查证据：旧线上公开设置无 `token_bank_enabled`，首页仍加载 `index-Beuvgwdz.js`；本地新版本为 `index-BhRPsipd.js`。根因是前一阶段只关闭渠道和 Nginx 接口，完整菜单总开关仍未发布，不是现有 Vue 菜单过滤失效。
- 对照现有功能开关，Token 银行同样由公开设置控制 NavItem.featureFlag；管理员「我的账户」和普通用户导航共享该过滤。浏览器本地实测关闭总开关后菜单即时消失，再开启恢复。
- 已发布原代码提交 `f121865c6`，镜像 `sub2api:20260925-f121865`。本地编译 linux/amd64，整包 SHA256 校验后载入，线上仅替换 app，保留 PostgreSQL/Redis/代理容器，无新迁移。健康 healthy / 0 restarts，运行版本 commit 核对一致。
- 线上 `token_bank_enabled=false` 明确入库；原两个渠道接收开关保持 false，原储蓄账号保持暂停，收益广播仍 false。后台配置接口、公开设置、HTML 首屏注入三处确认关闭。
- 线上已移除 vhost 对临时 `token-bank-disabled.conf` 的 include，nginx 检查和 reload 通过，改由已验证的应用总开关控制本人账号/授权接口。片段文件与版本存档保留作回滚参考。以后启停无需再改 Nginx。
- 公网页面使用已在本地验证的新前端资源，下载 SHA256 与本地一致，包含 `token_bank_enabled` 门控。旧浏览器会话需要刷新页面加载新资源；未登录账号及授权接口为 401，已登录关闭拦截由应用中间件负责（上一节本地真实 API 和单测覆盖 503）。
- 发布后真实 DeepSeek 小请求 200/有效 choices；Skoob OAuth 客户端启用、原密钥与原回调保持，管理接口正常、匿名 401；迁移数仍 292。银行未开放，真实 GPT/Claude 储蓄成功授权仍待验收。
- 回滚材料：`/root/backup-20260925-bank-menu-switch/` 保存原 compose 与 nginx；旧镜像另标记 `sub2api:rollback-20260925-bank-menu-switch`。回退应用时需同时恢复旧 nginx include，避免旧版无总开关的 API 暴露。
