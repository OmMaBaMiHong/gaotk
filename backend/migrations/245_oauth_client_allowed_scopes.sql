-- 245: OAuth 客户端增加 scope 白名单
-- 空 = 传统模式（保持既有行为：code 换全量面板令牌，存量客户端零影响）。
-- 非空 = 受限模式：authorize 校验 scope，token 端点签发受限短时令牌，
--        仅可访问 scope 白名单内的端点（/auth/me、会员态查询），面板管理 API 一律 403。

ALTER TABLE oauth_clients
    ADD COLUMN IF NOT EXISTS allowed_scopes TEXT NOT NULL DEFAULT '';
