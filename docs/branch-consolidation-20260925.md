# 2026-09-25 全部旧开发分支归并

## 范围

用户要求全部旧工作树未提交代码及所有自有开发分支归并至主分支。主分支实际名称为 main，自有远端为 gaotk；不上线，不运行生产迁移。

| 旧分支 | 纳入的提交/内容 |
| --- | --- |
| codex/membership-live | a1256ea40，会员人民币额度展示、支付接口额度及币种 |
| codex/skoob-membership-pricing | 22268a2，提交原有 7 个未提交文件：订阅缺失错误处理、回归测试、本地模拟脚本、原计划及验证记录 |
| codex/oauth-admin-settings | 8d85fcddb，动态 OAuth 回调设置、零余额免费分组访问及历史验证记录 |
| feat/skoob-oauth-server | 38c6a3e22，上游 0.2.8 与 OAuth Server 定制历史 |
| feat/oauth-client-admin | 5a81d3101，OAuth 应用注册表、管理接口、迁移和管理页面 |

这些分支及其原始提交先推送到自有仓库以保证冲突处理可回溯；全部归并后再清理分支引用。先前原目录会员 Key 过滤与 Token 银行设计文档已经在 main，继续保留。

## 合并兼容处理

- 免费分组保留零倍率，同时保留上游 simple-mode 创建分组的限制与规范化。
- Compose 沿用已经验证的 main 配置，保留 OAuth 和 simple-mode 环境变量，清除旧合并残留标记。
- Wire 重新生成，保留 Kimi/OpenCode Go 等原服务并接入 OAuth 应用管理。
- 旧 OAuth 回调设置与新 OAuth 应用管理共享默认客户端记录；首次迁移优先使用数据库已保存的回调，不退回旧环境回调。
- 保留旧回调校验的完整 URL/本地地址校验，防止字符串前缀判断错误放行伪造 localhost 地址。清空旧回调列表仍禁用回调。
- 增加回归测试，验证两处编辑入口保持一致、旧密钥保留、回调白名单清空及无效地址拒绝。
- 后端 API contract 测试适配新增 OAuth 客户端依赖。

## 旧本地环境

skoob-membership-pricing 的 .local-membership 下存在正在运行的本地模拟上游、后端与前端。该目录是本地运行数据，包含凭据，不进入 Git；保留目录与进程，工作树分支解除绑定后作为历史运行环境保存。开发统一回到 /Users/wade/work-space/sub2api 的 main。

本地模拟脚本与历史报告一并归档到 main；旧报告中的模拟通过及未解决边界保留原意，不作为当前生产状态证明，也不覆盖后来加入的会员分组 Key 限制规则。

## 验证

- 前端构建（含 i18n、Vue/TypeScript 与 Vite）通过。
- 支付计划卡、支付页、系统设置、OAuth 同意页：82 项测试通过。
- Docker 隔离数据库中执行全量迁移后，会员分组用途与扣费幂等两个集成测试通过。
- 后端编译通过；`go test -tags=unit ./cmd/server ./internal/domain ./internal/handler/... ./internal/server/... ./internal/service/... ./internal/repository/...` 全部通过，service 包耗时 191.135 秒。
- Compose 配置解析与 Git 空白检查通过。
- 不启动或更新生产服务。

知识图维护命令已执行，AST 扫描完成，但 graphify 因 64309 节点超出 HTML 可视化限制返回失败；不影响上述源码构建和测试结果。
