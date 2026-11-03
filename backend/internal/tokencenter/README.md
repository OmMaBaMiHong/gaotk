# TokenCenter 目录模块

复用 Chaos 已发布的契约、原生领域事实和中央榜单。使用 TOKENCENTER_DATABASE_URL 的专属目录库，账号/网关库不迁入此模块。

SourceFormat=chaos 的环境配置自动开启 RequireAdmission。同步须遍历全部快照分页，核对产品、版本、契约、总数、重复和每个原生对象资格后原子替换。保留规范 tagRefs、来源、报价条件与测评原值；不依据榜单创建不完整模型/站点，不把中转站名称当模型厂商。

access_status 保存在原 tokencenter_catalog 单表；明确撤回或契约待核后公开 API 返回结构化 code，SQL 重开也停读。普通短暂故障可以保留同契约的已合格快照，完整授权导入才恢复状态。严格模式管理员导入不能绕过中台。

验证：go test ./internal/tokencenter -count=1。真实数据库测试需要显式 TOKENCENTER_TEST_DATABASE_URL，且数据库名必须 tokencenter_test；测试仅创建并清理自己的临时 schema，覆盖迁移、保存、连接重开、非法导入、撤回、待核和恢复。不要提供账号数据库连接。

完整产品验收由 TokenCenter 与 Chaos 仓库的 2026-11-04 精确数据 P0 记录维护；本机通过不等于生产部署或真实中转质量测评完成。
