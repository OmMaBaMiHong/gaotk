#!/usr/bin/env python3
"""Run: python3 scripts/check_free_model_health.py (no live calls or database writes)."""
import copy
import json
import urllib.error
from unittest.mock import patch
import free_model_health as health

catalog = {"data": [{"id": health.MODEL, "isFree": True,
    "pricing": {"prompt": "0", "completion": "0", "request": "0", "discount": 0},
    "supported_parameters": ["tools"]}]}
assert health.catalog_ready(catalog)
for invalid in ({"data": []}, {"data": [{"id": health.MODEL}]}, {"data": [{"id": health.MODEL, "isFree": False}]}):
    assert not health.catalog_ready(invalid)
for field, value in (("prompt", "0.01"), ("completion", None), ("request", "1"), ("prompt", True), ("prompt", "NaN")):
    changed = copy.deepcopy(catalog)
    changed['data'][0]['pricing'][field] = value
    assert not health.catalog_ready(changed)
answer = {"model": health.MODEL, "choices": [{"message": {"content": "OK"}, "finish_reason": "stop"}]}
assert health.probe_ready(answer)
base_answer = dict(answer, model=health.MODEL[:-5])
assert health.probe_ready(base_answer)
assert not health.probe_ready(dict(answer, model="paid-model"))
assert not health.probe_ready({"model": health.MODEL, "choices": [{"message": {"reasoning_content": "thinking"}, "finish_reason": "stop"}]})
account = {"key": "fixture", "base": health.CHAT, "mapping": health.MODEL, "active": True}
with patch.object(health, 'sql', return_value=json.dumps(account)), patch.object(health, 'publish') as publish:
    with patch.object(health, 'request_json', side_effect=[catalog, answer]):
        health.main()
        publish.assert_called_with(True)
    with patch.object(health, 'request_json', return_value={"data": []}):
        health.main()
        publish.assert_called_with(False)
    with patch.object(health, 'request_json', side_effect=[catalog, answer]):
        health.main()
        publish.assert_called_with(True)
    for error in (TimeoutError(), urllib.error.HTTPError(health.CHAT, 429, "limited", {}, None)):
        with patch.object(health, 'request_json', side_effect=error):
            health.main()
            publish.assert_called_with(False)
print('free-model catalog, empty-answer, network error, shutdown and recovery checks passed')
