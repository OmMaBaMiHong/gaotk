# 开发与提交约定

- 本项目由用户单人开发，日常修改直接在原目录 `/Users/wade/work-space/sub2api` 的 `main` 分支进行。
- `main` 是我们自己仓库的主分支；用户所说的 Master 指这一主分支。
- 除非用户明确要求，不为新功能或修复另建分支或 worktree。
- 完成修改并验证后，提交到 `main` 并推送到 `gaotk/main`，不要长期留下未提交改动。
- 自有仓库是 `gaotk`（`https://github.com/OmMaBaMiHong/gaotk.git`）；`origin` 是开源上游，用于拉取更新，日常代码不要推到 `origin`。
- 删除已有代码前，先确认待删除版本已提交并推送，可以追溯。
- 用户要求归并旧工作区时，逐一提交并合入所有自有旧分支的独立改动，不能只处理当前目录；验证无遗漏并推送 main 后再清理分支引用。
- 保留未纳入 Git 的本地运行凭据和数据，不因清理分支中断正在运行的本地环境。

## 本地共享中间件（2026-10-18 用户裁决）

- 本机所有项目只共用一个 PostgreSQL、一个 Redis、一个 MinIO，包含原 Homebrew PostgreSQL。统一实例为 `local-postgres:5432`、`local-redis:6379`、`local-minio:9000`，宿主连接 `127.0.0.1` 对应端口；容器加入外部网络 `local-infra`。
- 基础设施由 `/Users/wade/work-space/local-infra/compose.yml` 独立管理。启动/检查使用 `docker compose -f /Users/wade/work-space/local-infra/compose.yml --env-file /Users/wade/work-space/local-infra/.env up -d` / `ps`。项目仅使用本地 `compose.shared.yml` 与已有连接配置，不创建私有 PG、Redis、MinIO 或额外测试栈；Homebrew PostgreSQL 服务及程序已卸载，禁止重新安装或恢复自启动。
- 按项目分数据库及账号、Redis 逻辑库、S3 桶及访问账号。Redis 逻辑库只隔离键名，不是安全沙箱；不得使用 `FLUSHALL`。新项目先在共享实例登记自己的库和账号，不复制中间件容器。
- 原卷及迁移备份必须保留；禁止 `down -v`、全局 prune 或以空库替代原账号和数据。共享卷是 external，不随业务项目删除。切换完成必须核对数据、扩展和真实连接，不能以 Compose 校验代替业务验收。
- 本地与线上复用业务代码，只区分部署配置。线上原单机/独立部署文件不受本地共享配置影响。Neo4j、FalkorDB、路由引擎等不同能力的服务按需使用，不当作普通 PG/Redis 数据删除；非当前使用的服务保持停止。
- Skoob 开发与真实回归只使用 master 的同一个 9002 统一应用容器，不另起预览端口或账号测试环境。
- 2026-10-18 用户选择保留 Skoob 原本地账号与作品。当前 `sub2api` 应用容器的 `127.0.0.1:8080` 连接共享 PG 的 `skoob_accounts` 和 Redis DB 5，为 Skoob 9002 提供原账号服务；原 `sub2api` / `sub2apipay` 数据库另行保留，两批账号不能混同。当前部署使用 `/Users/wade/work-space/local-infra/accounts.env`、`accounts.compose.yml` 覆盖本项目 `deploy/compose.shared.yml`，不能按默认配置自动将运行服务改绑到另一账号库或新建账号替代验证。
- Skoob 容器内部访问账号服务别名 `accounts.localhost:8080`，浏览器使用公开 IPv4 地址 `127.0.0.1:8080`，避免命中本机 WeKnora 已有 IPv6 8080。原 JWT、加密密钥和 OAuth 客户端继续保留；启动或更新前核对实际 Compose 标签、数据库名称与 Redis DB，不输出凭据。恢复和回退记录见 `/Users/wade/work-space/skoob/deploy/local-shared/README.md`。
