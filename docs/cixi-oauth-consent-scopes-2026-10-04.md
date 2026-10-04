# Cixi 接入发现的 OAuth 授权页范围问题

2026-10-04。复用现有 OAuthConsentView、账号状态核验、API 请求及中英文文案，没有新增登录实现。

Cixi 在真实 GaoTK 中注册为独立 OAuth 客户端，allowed_scopes 为 profile,keys。原确认页忽略查询参数 scope，展示固定的账号/会员/内容权限，并且 POST /oauth/authorize 没有传递 scope。服务器在缺省 scope 时使用客户端允许的范围；因此 Cixi 的服务器注册范围仍受限，但页面告知错误，其他允许多个范围的应用也会扩大为全部默认权限。

本次修改让显式范围决定页面文案，同时把该范围传给现有 API。profile/keys 展示账号及 Key 管理权限，profile 单独请求也能保留；不支持的范围不允许点授权。没有 scope 的历史链接保留既有行为，没有修改 Skoob 应用配置、签发协议或服务端范围校验。

验证：OAuthConsentView 10 项及语言键一致性 3 项通过，vue-tsc、Vite 构建及改动文件 ESLint 通过。ESLint 使用现有 pnpm 存储的 vue-eslint-parser 路径执行，没有变更依赖。新测试核验确认页文案和实际提交 payload，明确不代替真实用户授权。

代码提交到自有 main 并推送 gaotk。该源码修复尚未发布生产，线上仍可能显示旧会员/内容文案；本地 Cixi 的授权入口、正式客户端/回调/范围预检已通过。真实用户授权、选 Key、模型调用仍待验。
