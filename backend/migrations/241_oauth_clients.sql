-- OAuth 授权应用注册表：中转站作为 OAuth Server 的第三方应用客户端配置。
-- 取代旧的 OAUTH_SERVER_* 环境变量单客户端配置（见 auth_oauth_server.go 启动引导）。
CREATE TABLE IF NOT EXISTS oauth_clients (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    client_id VARCHAR(64) NOT NULL,
    client_secret VARCHAR(128) NOT NULL,
    redirect_uris TEXT NOT NULL DEFAULT '',
    allow_localhost BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    remark VARCHAR(500) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_clients_client_id ON oauth_clients(client_id);
