# OAuth 回调后台管理上线验收 · 2026-09-13

GaoTK（47.89.253.127）的 Sub2API 已从现有 0.2.1 / 34f3057f9 更新为 `sub2api:0.2.1-oauth-settings`。源代码提交 `640d46e` 已推送到自有 gaotk 仓库的 `codex/oauth-admin-settings` 分支；上游 origin 无写入权限，未修改上游。没有升级无关业务功能。

## 后台使用

打开 `https://gaotk.com/admin/settings` → **安全与认证** → **应用授权 · OAuth 回调**。显示 Client ID 和密钥是否已配置；密钥不返回浏览器。每行输入一个完整回调 URL，点击独立的“保存授权回调”，数据库保存后立即生效。

初次读取兼容 `OAUTH_SERVER_REDIRECT_URI`。首次保存后，以 `settings.oauth_server_redirect_uris` 的 JSON 数组为准；列表置空会关闭所有回调，不会恢复环境默认值。公网地址精确匹配 HTTPS；本地 HTTP 端口回调保持兼容。拒绝通配符、userinfo、片段和伪装 localhost；数据库故障时授权返回 503，不回落到可能已撤销的旧地址。

已通过真实管理页面保存并在数据库读取确认：

```
https://skoob.ai-ni.store/api/v1/account/oauth/callback
https://skoob.cc/api/v1/account/oauth/callback
```

## 验收证据

- `go test ./internal/handler ./internal/service -run TestOAuthServer -count=1` 通过，覆盖环境兼容、多地址、运行时变更、空名单、非法 URL、数据库故障，以及 GET/JSON 两个授权入口。
- `vue-tsc --noEmit`、完整 Docker 内前端构建与 Go embed 构建通过。
- `handler/admin` 和 `server/routes` 的测试编译失败来自已有测试桩签名落后，已在未修改的 34f3057f9 原工作区复现相同错误；本次未改无关测试或服务接口。
- 真实后台初次显示原环境回调；保存两条地址后显示“授权回调已保存并生效”和“当前使用后台保存的配置”。
- 两个允许地址的匿名授权请求均返回 302 到登录页；未知地址与 `http://localhost:123@unlisted.example/callback` 返回 400；匿名读取管理设置返回 401。
- 真实浏览器从 `https://skoob.cc/create` 点击登录，进入 GaoTK 授权页；授权后返回主站，账号菜单出现“退出登录”。Skoob 日志记录 callback / exchange 完成，app_user 从 0 变为 1。未制造令牌或绕过授权页面。
- 新应用容器健康；PostgreSQL、Redis 和已有 NetFountain 容器未重建。没有执行收费模型生成。

## 部署与回滚

- 部署目录：`/opt/sub2api`；只更新 `docker-compose.yml` 中 sub2api 的镜像，使用 `docker compose up -d --no-deps --no-build --pull never --wait sub2api`。
- 备份目录：`/opt/sub2api/backups/20260913-oauth-settings`，包含 PG 自定义格式快照、应用数据、原 Compose/env、容器配置与 SHA256。备份仅 root 可读；PG 归档目录解析通过（1263 行），未宣称完整恢复演练。
- 原镜像保留为 `sub2api:rollback-before-oauth-settings`；原应用 config image ID 为 `sha256:06023176969b4d4d381d3ca5b30b313dc443159e48ab33e85595f19968cec97e`。
- 新镜像 config digest：`sha256:009e97221413812aa80b665dba73c04c9def310cd2108d9c7b4a6e16b858d3cc`。
- 上传归档 SHA256：`8f805349e1bc4f3e8e0cd516d16a79639ad9ee70b0f0ffd3b5d8d4e150675fcd`，服务器校验一致。

回滚时恢复备份 Compose 或指定保留的旧镜像，仅重建 sub2api。旧程序会忽略新增数据库设置，重新使用原环境回调；回滚后 skoob.cc 登录将再次需要处理，因此不能将回滚视为无业务影响。新配置没有数据库结构迁移，禁止为回滚整库覆盖上线后产生的用户数据。
