#!/usr/bin/env python3
"""Single free-group health check. Run on the relay host via a 10-minute timer."""
import json
import subprocess
import urllib.error
import urllib.request
from decimal import Decimal, InvalidOperation

GROUP_ID = 16
ACCOUNT_ID = 112
MODEL = "stepfun/step-3.7-flash:free"
CATALOG = "https://api.kilo.ai/api/openrouter/models"
CHAT = "https://api.kilo.ai/api/openrouter/chat/completions"


def sql(statement):
    return subprocess.check_output([
        "docker", "exec", "-i", "sub2api-postgres", "sh", "-c",
        'exec psql -X -q -t -A -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"',
    ], input=statement.encode()).decode().strip()


def catalog_ready(body):
    for model in body.get("data", []):
        if model.get("id") != MODEL:
            continue
        price = model.get("pricing", {})
        if model.get("isFree") is not True or not {"prompt", "completion"}.issubset(price):
            return False
        try:
            if any(isinstance(v, bool) or Decimal(str(v)) != 0 for k, v in price.items() if k != "discount"):
                return False
        except (InvalidOperation, ValueError):
            return False
        return "tools" in model.get("supported_parameters", [])
    return False


def probe_ready(body):
    # The free provider may return the base ID, but never accept a different model.
    if body.get("model") not in (MODEL, MODEL[:-5]):
        return False
    for choice in body.get("choices", []):
        message = choice.get("message", {})
        text = message.get("content")
        if isinstance(text, str) and text.strip() and choice.get("finish_reason") == "stop":
            return True
    return False


def request_json(url, key=None, payload=None):
    headers = {"Accept": "application/json"}
    if key:
        headers["Authorization"] = "Bearer " + key
    data = None
    if payload is not None:
        data = json.dumps(payload).encode()
        headers["Content-Type"] = "application/json"
    with urllib.request.urlopen(urllib.request.Request(url, data=data, headers=headers), timeout=45) as response:
        return json.load(response)


def publish(healthy):
    status = "active" if healthy else "inactive"
    # ponytail: one monitored group; use the admin service if more groups need this policy.
    return sql("""
BEGIN;
SELECT pg_advisory_xact_lock(160112);
DO $guard$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM groups WHERE id=16 AND name='openskoob-free'
    AND deleted_at IS NULL AND rate_multiplier=0
    AND model_allowlist='{"enabled":true,"models":["stepfun/step-3.7-flash:free"]}'::jsonb) THEN
  RAISE EXCEPTION 'free group configuration changed; refusing to overwrite';
 END IF;
END $guard$;
WITH changed AS (
 UPDATE groups SET status='%s',updated_at=NOW() WHERE id=16 AND status IS DISTINCT FROM '%s' RETURNING id
), scheduling AS (
 INSERT INTO scheduler_outbox(event_type,group_id) SELECT 'group_changed',id FROM changed
)
INSERT INTO auth_cache_invalidation_outbox(cache_key)
SELECT encode(sha256(convert_to(k.key,'UTF8')),'hex') FROM api_keys k JOIN changed c ON c.id=k.group_id
WHERE k.deleted_at IS NULL AND k.key<>'';
COMMIT;
""" % (status, status))


def main():
    # Keep the upstream key in memory only; never write it to logs, argv or files.
    account = json.loads(sql("""SELECT json_build_object('key',credentials->>'api_key',
      'base',credentials->>'base_url','mapping',credentials->'model_mapping'->>'stepfun/step-3.7-flash:free',
      'active',status='active' AND schedulable) FROM accounts WHERE id=112 AND deleted_at IS NULL;"""))
    if account['base'] != CHAT or account['mapping'] != MODEL:
        raise RuntimeError('upstream configuration changed; refusing to send credentials')
    healthy, reason = False, "account_unavailable"
    if account['active'] and account['key']:
        try:
            if not catalog_ready(request_json(CATALOG)):
                reason = "catalog_missing_paid_or_incompatible"
            else:
                healthy = probe_ready(request_json(CHAT, account['key'], {
                    "model": MODEL, "messages": [{"role": "user", "content": "只回复 OK，不要解释。"}],
                    "max_tokens": 2048, "stream": False,
                }))
                reason = "healthy" if healthy else "probe_empty_or_wrong_model"
        except urllib.error.HTTPError as error:
            reason = "upstream_http_%s" % error.code
        except (OSError, ValueError, TypeError, AttributeError):
            reason = "upstream_check_failed"
    publish(healthy)
    print(json.dumps({"group_id": GROUP_ID, "model": MODEL, "healthy": healthy, "reason": reason}))


if __name__ == "__main__":
    main()
