# 月卡密钥绑定修复与发布

日期：2026-09-27。自有 main，修复提交 `d465ce019`。

## 原因与改动

线上 admin@example.com 的著作月卡订阅有效，分组 23 为 composite。自有提交 `1a4bed812` 将所有 subscription_plans.product_name=skoob 的分组判为仅会员权益，导致可选列表、密钥创建/修改以及模型鉴权同时拒绝访问。

删除这条限制及其专用分组查询、字段和缓存快照字段，恢复原有有效订阅授权。认证快照版本升至 26，让旧版本快照重新加载。有效期、额度、模型白名单、密钥归属等原有校验保持。

发布前 fetch 两个远端；gaotk/main 与本地一致，origin/main 为 a3eb7ef30，已全部包含，无待合并上游提交。原限制版本已在 gaotk/main 可追溯，未新建分支。

## 验证

- 修改预期的测试先复现：有订阅的 Skoob 分组被排除，OpenAI/Google 模型鉴权返回 403。
- 修复后 service/repository/middleware 的订阅、可选分组、认证缓存及模型白名单定向 unit 回归通过；最终完整 middleware unit 回归通过。
- CI=true 的真实数据库集成通过：skoob、大小写和空格标签、下架套餐、其他产品均遵循同一规则。无订阅拒绝，有效订阅可见并可创建/修改密钥，混合分组认证可加载，过期后不可见且拒绝创建/修改。
- 完整 Docker 构建通过，包括前端语言完整性、vue-tsc、Vite 与内嵌前端的 linux/amd64 后端。

## 线上发布

- 镜像 sub2api:20260927-d465ce0，运行二进制确认 commit d465ce0。
- 上传文件 SHA256：2b5b81c7fc6c1dbced57e11dfc00abae2dbcc85c20ae487918d283d2f9761c52，服务器校验一致。
- 备份 /root/backup-20260927-membership-key/：原 compose、私有 env、120890492 字节 PostgreSQL dump，pg_restore 目录校验 1298 行。回滚镜像 sub2api:rollback-20260927-membership-key。
- 只替换应用，数据库/Redis/代理保持，无新增迁移，迁移记录仍为 292。
- 公网 /health 200；公开 Token 银行总开关保持 false；匿名可选分组接口 401。
- 线上原 DeepSeek 密钥的 /v1/models 200，返回 3 个模型；旧启笔分组上的密钥仍为 403，已无 MEMBERSHIP_ONLY_GROUP 限制。著作分组 23 的订阅仍有效。
- 尚未取得用户浏览器登录会话，未宣称已在生产用该用户创建新密钥。原密钥 137 仍绑定过期分组 12，需用户编辑改绑分组 23 或新建密钥；本次未自动迁移原密钥。
