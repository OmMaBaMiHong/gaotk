-- GaoTK production group 15: public names are canonical; upstream IDs belong
-- to accounts. Back up these two model_mapping fields before execution.
-- After commit publish `refresh` on Redis channel_cache_updated; the scheduler
-- consumes the outbox event and /v1/models expires within 30 seconds.
\set ON_ERROR_STOP on
BEGIN;
DO $$
DECLARE
  account_mapping jsonb;
  channel_mapping jsonb;
BEGIN
  SELECT credentials->'model_mapping' INTO account_mapping
    FROM accounts WHERE id = 107 AND platform = 'deepseek' AND deleted_at IS NULL FOR UPDATE;
  SELECT model_mapping INTO channel_mapping
    FROM channels WHERE id = 3 FOR UPDATE;
  IF account_mapping IS DISTINCT FROM '{"deepseek-v4-pro":"deepseek-v4-pro-ga-260813","deepseek-v4-flash":"deepseek-v4-flash-ga-260731","deepseek-v4-1-flash":"deepseek-v4-1-flash-260910"}'::jsonb
     OR channel_mapping IS DISTINCT FROM '{"deepseek":{"deepseek-v4-pro":"deepseek-v4-pro-ga-260813","deepseek-v4-flash":"deepseek-v4-flash-ga-260731"}}'::jsonb
  THEN
    RAISE EXCEPTION 'Configuration changed; inspect before applying this patch';
  END IF;
  IF (SELECT array_agg(group_id ORDER BY group_id) FROM account_groups WHERE account_id = 107) IS DISTINCT FROM ARRAY[15]::bigint[]
     OR NOT EXISTS (SELECT 1 FROM channel_groups WHERE channel_id = 3 AND group_id = 15)
  THEN
    RAISE EXCEPTION 'Unexpected account/channel group binding';
  END IF;

  UPDATE accounts
    SET credentials = jsonb_set(credentials, '{model_mapping}',
      '{"deepseek-flash":"deepseek-v4-1-flash-260910","deepseek-v4-pro":"deepseek-v4-pro-ga-260813"}'::jsonb),
      updated_at = NOW()
    WHERE id = 107;
  -- Legacy public names normalize before account selection. Provider-specific
  -- dated model IDs must never be a group-wide routing target.
  UPDATE channels
    SET model_mapping = '{"deepseek":{"deepseek-v4-flash":"deepseek-flash","deepseek-v4-1-flash":"deepseek-flash"}}'::jsonb,
      updated_at = NOW()
    WHERE id = 3;
  INSERT INTO scheduler_outbox (event_type, account_id, payload)
    VALUES ('account_changed', 107, '{"group_ids":[15]}'::jsonb);
END $$;
COMMIT;
