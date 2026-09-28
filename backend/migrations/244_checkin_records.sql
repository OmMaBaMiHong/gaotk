-- 244: 每日签到发放记录
-- (user_id, checkin_date) 唯一索引是防并发重复签到的正确性来源：
-- 并发请求后到的 INSERT 违反唯一索引，事务回滚，余额不会被多发。

CREATE TABLE IF NOT EXISTS checkin_records (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL,
    checkin_date    VARCHAR(10) NOT NULL,                -- 服务器本地日期 YYYY-MM-DD
    amount_awarded  NUMERIC(20, 8) NOT NULL,             -- 发放的美元额度（users.balance 同口径）
    streak_days     INT NOT NULL DEFAULT 1,              -- 连续签到天数（含当天）
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_checkin_records_user_date
    ON checkin_records (user_id, checkin_date);

CREATE INDEX IF NOT EXISTS idx_checkin_records_user_created
    ON checkin_records (user_id, created_at DESC);
