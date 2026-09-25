# 普通用户免费调用的零余额验证

## 改动边界

Skoob 自动为已登录用户用自己的 OAuth / 登录令牌复用或创建 openskoob-free（group 16）Key，不使用管理员凭证。group 16 只有免费上游 109、112，无付费回退，标准分组倍率改为 0；本次不改其他分组。

中转站保留用户状态、Key 状态/有效期/配额、RPM 等校验，仅明确零倍率、无独立图片/视频倍率的标准分组跳过余额要求。创建/编辑分组允许倍率 0；负数仍拒绝，用户个别倍率覆盖校验保持原样。

## 验证

- 先增加普通用户 balance=0 的鉴权与计费测试，观察原实现拒绝免费组，再修正并通过。
- `go test ./internal/service -run TestFreeStandardGroupBillingEligibility -count=1` 通过。
- `go test -tags unit ./internal/server/middleware -run 'TestZeroRateStandardGroup|TestAPIKeyAuthRejectsOversized|TestSimpleModeBypassesQuotaCheck' -count=1` 通过。
- `docker buildx build --platform linux/amd64 --load -t sub2api:0.2.1-free-access .` 通过，包含后台倍率输入框 min=0。
- 生产容器 sub2api 健康，/health 返回 ok；仅重建应用，PostgreSQL / Redis 保留。
- 专用验收用户 151：role=user、balance=0、0 条订阅。由数据库建立测试身份，然后通过真实登录接口取得令牌；没有测试邮箱注册，也没有发邮件。
- 用户自己的自动 Key 133 属于 group 16，多次 ensure 后仍只有 1 把。Nex N2.5 Pro Free 的真实请求记录 7181、7182：actual_cost=0，rate_multiplier=0。

全量 `-tags unit ./internal/service` 仍有既有测试签名不匹配（UpdateUserBalance、NewTokenRefreshService）；本次未修改这些无关测试。

## 发布与回滚

生产目录 /opt/sub2api；已保留 docker-compose.yml.before-free-access 和 group16.before-free-access.json（权限 600）。
发布镜像 sub2api:0.2.1-free-access；回滚镜像 sub2api:0.2.1-oauth-settings。回滚前先关闭 Skoob models.free-access.enabled，按备份恢复应用镜像与 group 16 原倍率，再健康检查。不得清空数据库或 Redis。
