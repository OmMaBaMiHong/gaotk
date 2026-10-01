"""Run: python3 backend/migrations/tests/check_token_savings_guard.py POSTGRES_CONTAINER"""
import pathlib
import subprocess
import sys

migration = (pathlib.Path(__file__).parents[1] / "246_token_savings_guard_group_alias.sql").read_text()
sql = """
BEGIN;
CREATE TEMP TABLE accounts(id bigint PRIMARY KEY, owner_user_id bigint, parent_account_id bigint, platform text);
CREATE TEMP TABLE account_groups(account_id bigint, group_id bigint);
CREATE TEMP TABLE groups(id bigint PRIMARY KEY, platform text, name text);
SET LOCAL search_path = pg_temp, public;
""" + migration + migration + """
CREATE TRIGGER guard BEFORE INSERT OR UPDATE ON accounts FOR EACH ROW EXECUTE FUNCTION pg_temp.token_savings_account_guard();
CREATE TRIGGER guard BEFORE INSERT OR UPDATE ON account_groups FOR EACH ROW EXECUTE FUNCTION pg_temp.token_savings_account_guard();
CREATE TRIGGER guard BEFORE UPDATE ON groups FOR EACH ROW EXECUTE FUNCTION pg_temp.token_savings_account_guard();
INSERT INTO groups VALUES(1,'openai','free'),(2,'gemini','other');
INSERT INTO accounts VALUES(10,7,NULL,'openai');
INSERT INTO account_groups VALUES(10,1);
UPDATE groups SET name='curated' WHERE id=1;
DO $$
BEGIN
 IF (SELECT name FROM groups WHERE id=1) <> 'curated' THEN RAISE EXCEPTION 'ordinary save failed'; END IF;
 BEGIN
  UPDATE groups SET platform='gemini' WHERE id=1;
  RAISE EXCEPTION 'platform guard was bypassed';
 EXCEPTION WHEN raise_exception THEN
  IF SQLERRM <> 'savings group platform mismatch' THEN RAISE; END IF;
 END;
 BEGIN
  INSERT INTO account_groups VALUES(10,2);
  RAISE EXCEPTION 'group binding guard was bypassed';
 EXCEPTION WHEN raise_exception THEN
  IF SQLERRM <> 'savings account group platform mismatch' THEN RAISE; END IF;
 END;
 BEGIN
  UPDATE accounts SET owner_user_id=NULL WHERE id=10;
  RAISE EXCEPTION 'ownership guard was bypassed';
 EXCEPTION WHEN raise_exception THEN
  IF SQLERRM <> 'savings account ownership is immutable' THEN RAISE; END IF;
 END;
END $$;
ROLLBACK;
"""
subprocess.run(["rtk", "proxy", "docker", "exec", "-i", sys.argv[1], "sh", "-c",
                'exec psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'],
               input=sql, text=True, check=True)
print("Group save, migration idempotence, platform and ownership guards passed; transaction rolled back")
