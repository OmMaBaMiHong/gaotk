-- The row variable a and the groups-branch table alias a conflicted in PL/pgSQL.
-- Rename only the table alias; retain ownership and platform checks.
CREATE OR REPLACE FUNCTION token_savings_account_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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
   SELECT 1 FROM account_groups ag JOIN accounts linked_account ON linked_account.id=ag.account_id
   JOIN accounts o ON o.id=COALESCE(linked_account.parent_account_id,linked_account.id)
   WHERE ag.group_id=NEW.id AND o.owner_user_id IS NOT NULL AND linked_account.platform<>NEW.platform
  ) THEN RAISE EXCEPTION 'savings group platform mismatch'; END IF;
 END IF;
 RETURN NEW;
END $$;
