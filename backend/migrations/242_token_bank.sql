CREATE TABLE account_rental_policies (
 id BIGSERIAL PRIMARY KEY,
 platform VARCHAR(50) NOT NULL UNIQUE CHECK (platform IN ('openai','anthropic','deepseek')),
 group_id BIGINT NOT NULL UNIQUE REFERENCES groups(id),
 owner_share_bps INTEGER NOT NULL DEFAULT 8000 CHECK (owner_share_bps BETWEEN 0 AND 10000),
 admin_user_id BIGINT NOT NULL REFERENCES users(id),
 enabled BOOLEAN NOT NULL DEFAULT false,
 version INTEGER NOT NULL DEFAULT 1,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE accounts ADD COLUMN owner_user_id BIGINT REFERENCES users(id),
 ADD COLUMN rental_policy_id BIGINT REFERENCES account_rental_policies(id),
 ADD COLUMN rental_status VARCHAR(20) NOT NULL DEFAULT '',
 ADD COLUMN rental_identity VARCHAR(128);
ALTER TABLE accounts ADD CONSTRAINT account_rental_complete CHECK (
 (owner_user_id IS NULL AND rental_policy_id IS NULL AND rental_status = '' AND rental_identity IS NULL)
 OR (owner_user_id IS NOT NULL AND rental_policy_id IS NOT NULL AND rental_status IN ('active','paused') AND rental_identity IS NOT NULL)
);
CREATE UNIQUE INDEX accounts_rental_identity_unique ON accounts(rental_identity) WHERE rental_identity IS NOT NULL;
CREATE INDEX accounts_rental_owner ON accounts(owner_user_id) WHERE owner_user_id IS NOT NULL;
CREATE TABLE account_revenue_ledger (
 id BIGSERIAL PRIMARY KEY,
 request_id VARCHAR(255) NOT NULL,
 api_key_id BIGINT NOT NULL,
 account_id BIGINT NOT NULL,
 owner_account_id BIGINT NOT NULL,
 owner_user_id BIGINT NOT NULL,
 admin_user_id BIGINT NOT NULL,
 policy_id BIGINT NOT NULL,
 policy_version INTEGER NOT NULL,
 platform VARCHAR(50) NOT NULL,
 group_id BIGINT NOT NULL,
 model TEXT NOT NULL,
 billing_type SMALLINT NOT NULL,
 input_tokens BIGINT NOT NULL,
 output_tokens BIGINT NOT NULL,
 cache_tokens BIGINT NOT NULL,
 bill_amount NUMERIC(20,8) NOT NULL,
 owner_share_bps INTEGER NOT NULL,
 owner_amount NUMERIC(20,8) NOT NULL,
 admin_amount NUMERIC(20,8) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE (request_id, api_key_id),
 CHECK (bill_amount >= 0 AND owner_amount >= 0 AND admin_amount >= 0 AND owner_amount + admin_amount = bill_amount)
);
CREATE INDEX account_revenue_owner_time ON account_revenue_ledger(owner_user_id,created_at DESC);
CREATE INDEX account_revenue_account_time ON account_revenue_ledger(owner_account_id,created_at DESC);
-- Guard all account management entry points, including admin imports and shadows.
CREATE FUNCTION token_bank_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a accounts%ROWTYPE; p account_rental_policies%ROWTYPE; parent accounts%ROWTYPE;
BEGIN
 IF TG_TABLE_NAME = 'account_groups' THEN
  SELECT * INTO a FROM accounts WHERE id = NEW.account_id;
  IF EXISTS(SELECT 1 FROM account_rental_policies WHERE group_id=NEW.group_id AND platform<>a.platform) THEN RAISE EXCEPTION 'rental pool platform mismatch'; END IF;
  IF a.parent_account_id IS NOT NULL THEN SELECT * INTO parent FROM accounts WHERE id=a.parent_account_id; IF parent.owner_user_id IS NOT NULL THEN a := parent; END IF; END IF;
  IF a.owner_user_id IS NOT NULL THEN
   SELECT * INTO p FROM account_rental_policies WHERE id=a.rental_policy_id;
   IF NEW.group_id <> p.group_id THEN RAISE EXCEPTION 'rental account must use its platform pool'; END IF;
  END IF;
 ELSIF TG_TABLE_NAME = 'accounts' THEN
  IF TG_OP='UPDATE' AND OLD.owner_user_id IS NOT NULL AND (NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id OR NEW.rental_policy_id IS DISTINCT FROM OLD.rental_policy_id OR NEW.rental_identity IS DISTINCT FROM OLD.rental_identity) THEN RAISE EXCEPTION 'rental account ownership is immutable'; END IF;
  IF NEW.parent_account_id IS NOT NULL THEN
   SELECT * INTO parent FROM accounts WHERE id=NEW.parent_account_id;
   IF NEW.owner_user_id IS NOT NULL THEN RAISE EXCEPTION 'rental owner must be the credential parent'; END IF;
   IF parent.owner_user_id IS NOT NULL AND (NEW.platform<>parent.platform OR EXISTS(SELECT 1 FROM account_groups ag JOIN account_rental_policies rp ON rp.id=parent.rental_policy_id WHERE ag.account_id=NEW.id AND ag.group_id<>rp.group_id)) THEN RAISE EXCEPTION 'rental shadow platform or pool mismatch'; END IF;
  END IF;
  IF TG_OP='UPDATE' AND OLD.parent_account_id IS DISTINCT FROM NEW.parent_account_id AND EXISTS(SELECT 1 FROM accounts WHERE id=OLD.parent_account_id AND owner_user_id IS NOT NULL) THEN RAISE EXCEPTION 'rental shadow parent is immutable'; END IF;
  IF NEW.owner_user_id IS NOT NULL THEN
   SELECT * INTO p FROM account_rental_policies WHERE id=NEW.rental_policy_id;
   IF EXISTS(SELECT 1 FROM account_groups WHERE account_id=NEW.id AND group_id<>p.group_id) THEN RAISE EXCEPTION 'rental account pool mismatch'; END IF;
   IF NEW.platform <> p.platform THEN RAISE EXCEPTION 'rental account platform mismatch'; END IF;
   IF NEW.rental_status <> 'active' THEN NEW.schedulable := false; END IF;
  END IF;
 ELSIF TG_TABLE_NAME='account_rental_policies' THEN
  IF NOT EXISTS(SELECT 1 FROM groups WHERE id=NEW.group_id AND platform=NEW.platform AND deleted_at IS NULL) OR EXISTS(SELECT 1 FROM account_groups ag JOIN accounts ac ON ac.id=ag.account_id WHERE ag.group_id=NEW.group_id AND ac.platform<>NEW.platform) THEN RAISE EXCEPTION 'rental policy pool platform mismatch'; END IF;
  IF TG_OP='UPDATE' AND (NEW.platform<>OLD.platform OR NEW.group_id<>OLD.group_id) AND EXISTS(SELECT 1 FROM accounts WHERE rental_policy_id=OLD.id) THEN RAISE EXCEPTION 'occupied rental policy pool is immutable'; END IF;
 ELSIF TG_TABLE_NAME='groups' THEN
  IF NEW.platform IS DISTINCT FROM OLD.platform AND EXISTS(SELECT 1 FROM account_rental_policies WHERE group_id=NEW.id) THEN RAISE EXCEPTION 'rental pool platform is immutable'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER token_bank_account_group BEFORE INSERT OR UPDATE ON account_groups FOR EACH ROW EXECUTE FUNCTION token_bank_guard();
CREATE TRIGGER token_bank_account BEFORE INSERT OR UPDATE ON accounts FOR EACH ROW EXECUTE FUNCTION token_bank_guard();
CREATE TRIGGER token_bank_group BEFORE UPDATE ON groups FOR EACH ROW EXECUTE FUNCTION token_bank_guard();

CREATE TRIGGER token_bank_policy BEFORE INSERT OR UPDATE ON account_rental_policies FOR EACH ROW EXECUTE FUNCTION token_bank_guard();

-- Check at commit so existing atomic "delete then rebind groups" operations
-- remain valid, while an empty pool cannot route a rental as an ungrouped account.
CREATE FUNCTION token_bank_check_membership(account_id_to_check BIGINT) RETURNS void LANGUAGE plpgsql AS $$
DECLARE pool_id BIGINT;
BEGIN
 SELECT p.group_id INTO pool_id FROM accounts a
 JOIN accounts owner ON owner.id=COALESCE(a.parent_account_id,a.id)
 JOIN account_rental_policies p ON p.id=owner.rental_policy_id
 WHERE a.id=account_id_to_check AND a.deleted_at IS NULL;
 IF FOUND AND NOT EXISTS(SELECT 1 FROM account_groups WHERE account_id=account_id_to_check AND group_id=pool_id) THEN
  RAISE EXCEPTION 'rental account must remain in its platform pool';
 END IF;
END $$;
CREATE FUNCTION token_bank_membership_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='accounts' THEN
  PERFORM token_bank_check_membership(NEW.id);
 ELSE
  IF TG_OP<>'INSERT' THEN PERFORM token_bank_check_membership(OLD.account_id); END IF;
  IF TG_OP<>'DELETE' THEN PERFORM token_bank_check_membership(NEW.account_id); END IF;
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER token_bank_membership_account AFTER INSERT OR UPDATE ON accounts
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
 WHEN (NEW.owner_user_id IS NOT NULL OR NEW.parent_account_id IS NOT NULL)
 EXECUTE FUNCTION token_bank_membership_guard();
CREATE CONSTRAINT TRIGGER token_bank_membership_group AFTER INSERT OR UPDATE OR DELETE ON account_groups
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION token_bank_membership_guard();
