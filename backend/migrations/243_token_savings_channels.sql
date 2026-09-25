-- Replace the previous parallel rental configuration with existing channels.
-- Keep migration 242 and historical ledger rows intact for upgraded databases.
DROP TRIGGER token_bank_membership_account ON accounts;
DROP TRIGGER token_bank_membership_group ON account_groups;
DROP TRIGGER token_bank_account_group ON account_groups;
DROP TRIGGER token_bank_account ON accounts;
DROP TRIGGER token_bank_group ON groups;
DROP TRIGGER token_bank_policy ON account_rental_policies;
DROP FUNCTION token_bank_membership_guard();
DROP FUNCTION token_bank_check_membership(BIGINT);
DROP FUNCTION token_bank_guard();
ALTER TABLE accounts DROP CONSTRAINT account_rental_complete;

-- Retain configured test/early-adopter policies by moving them to channels.
INSERT INTO channels(name,description,status)
SELECT 'Token 储蓄 · ' || p.platform || ' · ' || p.id,
       '由原储蓄规则迁移，使用原渠道与账号管理', 'active'
FROM account_rental_policies p
WHERE NOT EXISTS (SELECT 1 FROM channel_groups cg WHERE cg.group_id=p.group_id);
INSERT INTO channel_groups(channel_id,group_id)
SELECT c.id,p.group_id FROM account_rental_policies p
JOIN channels c ON c.name='Token 储蓄 · ' || p.platform || ' · ' || p.id
WHERE NOT EXISTS (SELECT 1 FROM channel_groups cg WHERE cg.group_id=p.group_id);

DO $$ BEGIN
 IF EXISTS (
  SELECT cg.channel_id FROM account_rental_policies p JOIN channel_groups cg ON cg.group_id=p.group_id
  GROUP BY cg.channel_id HAVING COUNT(DISTINCT (p.owner_share_bps,p.admin_user_id,p.enabled))>1
 ) THEN
  RAISE EXCEPTION 'Existing savings policies in the same channel have different shares; align their settings before upgrading';
 END IF;
END $$;
UPDATE channels c SET features_config=COALESCE(c.features_config,'{}'::jsonb) || jsonb_build_object('token_savings',m.config)
FROM (
 SELECT cg.channel_id,jsonb_build_object(
  'enabled',bool_or(p.enabled),'owner_share_bps',min(p.owner_share_bps),
  'admin_user_id',min(p.admin_user_id),'receiving_group_ids',jsonb_agg(p.group_id ORDER BY p.group_id)
 ) config FROM account_rental_policies p JOIN channel_groups cg ON cg.group_id=p.group_id GROUP BY cg.channel_id
) m WHERE m.channel_id=c.id;

ALTER TABLE account_revenue_ledger ADD COLUMN channel_id BIGINT;
UPDATE account_revenue_ledger l SET channel_id=cg.channel_id
FROM account_rental_policies p JOIN channel_groups cg ON cg.group_id=p.group_id WHERE l.policy_id=p.id;
ALTER TABLE account_revenue_ledger ALTER COLUMN policy_id DROP NOT NULL, ALTER COLUMN policy_version DROP NOT NULL;
ALTER TABLE accounts DROP COLUMN rental_policy_id, DROP COLUMN rental_status, DROP COLUMN rental_identity;
DROP TABLE account_rental_policies;

-- Preserve user ownership and platform matching without restricting the account
-- to one group. Original account status/schedulable fields manage availability.
CREATE FUNCTION token_savings_account_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner_id BIGINT; account_platform VARCHAR; a accounts%ROWTYPE;
BEGIN
 IF TG_TABLE_NAME='account_groups' THEN
  SELECT * INTO a FROM accounts WHERE id=NEW.account_id;
  SELECT owner_user_id INTO owner_id FROM accounts WHERE id=COALESCE(a.parent_account_id,a.id);
  IF owner_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM groups WHERE id=NEW.group_id AND platform=a.platform) THEN
   RAISE EXCEPTION 'savings account group platform mismatch';
  END IF;
 ELSIF TG_TABLE_NAME='accounts' THEN
  IF TG_OP='UPDATE' AND OLD.owner_user_id IS NOT NULL AND NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id THEN
   RAISE EXCEPTION 'savings account ownership is immutable';
  END IF;
  owner_id := NEW.owner_user_id;
  IF NEW.parent_account_id IS NOT NULL THEN
   SELECT owner_user_id,platform INTO owner_id,account_platform FROM accounts WHERE id=NEW.parent_account_id;
   IF owner_id IS NOT NULL AND (NEW.platform<>account_platform OR NEW.owner_user_id IS NOT NULL) THEN
    RAISE EXCEPTION 'savings shadow must inherit its parent identity';
   END IF;
  END IF;
  IF TG_OP='UPDATE' AND OLD.parent_account_id IS DISTINCT FROM NEW.parent_account_id AND EXISTS(SELECT 1 FROM accounts WHERE id=OLD.parent_account_id AND owner_user_id IS NOT NULL) THEN
   RAISE EXCEPTION 'savings shadow parent is immutable';
  END IF;
  IF owner_id IS NOT NULL AND EXISTS(SELECT 1 FROM account_groups ag JOIN groups g ON g.id=ag.group_id WHERE ag.account_id=NEW.id AND g.platform<>NEW.platform) THEN
   RAISE EXCEPTION 'savings account group platform mismatch';
  END IF;
 ELSE
  IF NEW.platform IS DISTINCT FROM OLD.platform AND EXISTS(
   SELECT 1 FROM account_groups ag JOIN accounts a ON a.id=ag.account_id
   JOIN accounts o ON o.id=COALESCE(a.parent_account_id,a.id)
   WHERE ag.group_id=NEW.id AND o.owner_user_id IS NOT NULL AND a.platform<>NEW.platform
  ) THEN RAISE EXCEPTION 'savings group platform mismatch'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER token_savings_account BEFORE INSERT OR UPDATE ON accounts FOR EACH ROW EXECUTE FUNCTION token_savings_account_guard();
CREATE TRIGGER token_savings_account_group BEFORE INSERT OR UPDATE ON account_groups FOR EACH ROW EXECUTE FUNCTION token_savings_account_guard();
CREATE TRIGGER token_savings_group BEFORE UPDATE ON groups FOR EACH ROW EXECUTE FUNCTION token_savings_account_guard();

-- Config changes refresh the existing scheduler projections, including shadows.
CREATE FUNCTION token_savings_channel_refresh() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='channels' THEN
 IF TG_OP<>'DELETE' THEN
 IF COALESCE((NEW.features_config->'token_savings'->>'enabled')::boolean,false) THEN
  IF NOT EXISTS(SELECT 1 FROM users WHERE id=(NEW.features_config->'token_savings'->>'admin_user_id')::bigint AND role='admin' AND status='active' AND deleted_at IS NULL) THEN
   RAISE EXCEPTION 'savings recipient must be an active administrator';
  END IF;
 END IF;
 END IF;
 END IF;
 INSERT INTO scheduler_outbox(event_type,account_id)
 SELECT 'account_changed',a.id FROM accounts a JOIN accounts o ON o.id=COALESCE(a.parent_account_id,a.id)
 WHERE o.owner_user_id IS NOT NULL AND a.deleted_at IS NULL;
 RETURN NULL;
END $$;
CREATE TRIGGER token_savings_channel AFTER INSERT OR UPDATE OR DELETE ON channels FOR EACH ROW EXECUTE FUNCTION token_savings_channel_refresh();
CREATE TRIGGER token_savings_channel_groups AFTER INSERT OR UPDATE OR DELETE ON channel_groups FOR EACH ROW EXECUTE FUNCTION token_savings_channel_refresh();

INSERT INTO scheduler_outbox(event_type,account_id)
SELECT 'account_changed',a.id FROM accounts a JOIN accounts o ON o.id=COALESCE(a.parent_account_id,a.id)
WHERE o.owner_user_id IS NOT NULL AND a.deleted_at IS NULL;
