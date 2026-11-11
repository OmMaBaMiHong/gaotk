# TokenCenter 目录模块

复用 Chaos 已发布的契约、原生领域事实和中央榜单。使用 TOKENCENTER_DATABASE_URL 的专属目录库，账号/网关库不迁入此模块。

集合导航词表来自嵌入的 Chaos 契约 `tokencenter-collection-vocabulary-v1.contract.json`（声明提交 `9de086c159179dfca2eaf65099027b1176d8d61b`）。成功的目录响应增加 `collectionVocabulary`：slug、显示名、`navigation` 维度、修订，以及 `topNav`、`memberSlugs`、`parentSlug`、`surface`、`boardRule`、`billingUnit`、`purchaseSurface`、`governanceStatus`、`externalFilterSlugs`。数字 `id` 只在 Chaos 源已配置且同源 `GET /public/v1/tags?dimension=navigation` 发布了 Tag 时回填。公开投影只有 active 与 legacy；`tc-reserved` 草稿留在契约文件中。快照不保存这份投影，也不改 `providerKind` 或已有 `tags` / `tagRefs`。字段对应见 `docs/tokencenter.md`。

SourceFormat=chaos 的环境配置自动开启 RequireAdmission。同步须遍历全部快照分页，核对产品、版本、契约、总数、重复和每个原生对象资格后原子替换。保留规范 tagRefs、来源、报价条件与测评原值；不依据榜单创建不完整模型/站点，不把中转站名称当模型厂商。

access_status 保存在原 tokencenter_catalog 单表；明确撤回或契约待核后公开 API 返回结构化 code，SQL 重开也停读。普通短暂故障可以保留同契约的已合格快照，完整授权导入才恢复状态。严格模式管理员导入不能绕过中台。

验证：go test ./internal/tokencenter -count=1。真实数据库测试需要显式 TOKENCENTER_TEST_DATABASE_URL，且数据库名必须 tokencenter_test；测试仅创建并清理自己的临时 schema，覆盖迁移、保存、连接重开、非法导入、撤回、待核和恢复。不要提供账号数据库连接。

完整产品验收由 TokenCenter 与 Chaos 仓库的 2026-11-04 精确数据 P0 记录维护；本机通过不等于生产部署或真实中转质量测评完成。

## 联系入驻

匿名 POST `/api/v1/tokencenter/applications` 登记中转站收录或平台合作线索，返回仅显示一次的查询凭证。POST `/api/v1/tokencenter/applications/status` 使用凭证查询公开处理结果；凭证摘要入库，联系方式只在已鉴权后台读取。

管理员 GET `/api/v1/admin/tokencenter/applications`（status/before 分页），POST `/api/v1/admin/tokencenter/applications/:id/review`（revision/status/note/siteId）。复用原 admin 中间件；不创建第二套账号。更新状态同时追加审计，旧 revision 返回409。published 必须关联当前非演示、具备 Chaos 准入契约且官网相同的正式中转站。

线索独立保存在数字主键的 tokencenter_applications；不直接写 tokencenter_catalog，不授予商家所有权，不发送站外消息。来源登记、原生适配、类型校验与正式发布仍归 Chaos。当前中转阶段仅做站点和报价，测速与质量测评后置。
